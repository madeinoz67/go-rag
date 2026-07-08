/* app.js — Alpine root component for the go-rag management console.
 * Slice 0 (spec 046). Pure vanilla JS; the only external dep is Alpine itself,
 * which calls goragApp() to bootstrap <body x-data="goragApp()">.
 *
 * Auth model (per spec 046 contracts/ui-transport.md):
 *   - Opaque `gorags_` Bearer token held in memory (this.token).
 *   - Optionally mirrored to localStorage under 'goragToken' for tab-persistence.
 *   - The server NEVER issues Set-Cookie; the client NEVER reads document.cookie.
 *   - On any 401, the token is cleared and the login screen is shown.
 *
 * No external deps beyond Alpine. No cookies. No build step.
 */

(function () {
  'use strict';

  var GORAG_TOKEN_KEY = 'goragToken';
  // Same-origin by default; empty string keeps fetch paths relative.
  var GORAG_API_BASE = '';

  function goragApp() {
    return {
      // === State ============================================================
      token: '',
      currentView: 'dashboard',
      stats: {},
      login: { username: '', password: '' },
      error: '',
      loading: false,

      // === Auth gate ========================================================

      /** True when a session token is held in memory. */
      isAuthed: function () {
        return !!this.token;
      },

      /** Alpine x-init hook — read token from storage, then prime the dashboard. */
      mount: function () {
        var stored = null;
        try {
          if (window.localStorage) {
            stored = localStorage.getItem(GORAG_TOKEN_KEY);
          }
        } catch (_e) {
          // storage disabled (private mode, etc.) — fall through to login
        }
        if (stored) {
          this.token = stored;
        }
        if (this.isAuthed()) {
          this.loadDashboard();
        }
      },

      // === Network ==========================================================

      /** Central fetch helper.
       *  - Injects `Authorization: Bearer <token>` when a token is held.
       *  - JSON-encodes the body when a body is supplied without an explicit
       *    Content-Type.
       *  - On any 401: clears the token (and storage mirror) so the gate
       *    re-locks. The caller can branch on `res.status === 401`.
       *  - credentials: 'omit' so the browser never sends/receives cookies.
       */
      api: async function (path, opts) {
        opts = opts || {};
        var headers = Object.assign({}, opts.headers || {});
        headers['Accept'] = 'application/json';
        if (opts.body && !('Content-Type' in headers)) {
          headers['Content-Type'] = 'application/json';
        }
        if (this.token) {
          headers['Authorization'] = 'Bearer ' + this.token;
        }

        var res;
        try {
          res = await fetch(GORAG_API_BASE + path, {
            method: opts.method || 'GET',
            headers: headers,
            body: opts.body,
            credentials: 'omit', // never send/receive cookies
          });
        } catch (networkErr) {
          this.error = 'Network error: cannot reach go-rag daemon.';
          throw networkErr;
        }

        if (res.status === 401) {
          // Session gone / invalid / never existed — re-lock the gate.
          this.clearToken();
          this.error = '';
          return res;
        }
        return res;
      },

      // === Login / logout ===================================================

      /** POST /login → on 200 hold the opaque session token, then load dashboard. */
      submitLogin: async function () {
        this.error = '';
        this.loading = true;
        try {
          var res = await fetch(GORAG_API_BASE + '/login', {
            method: 'POST',
            headers: {
              'Content-Type': 'application/json',
              'Accept': 'application/json',
            },
            body: JSON.stringify({
              username: this.login.username,
              password: this.login.password,
            }),
            credentials: 'omit',
          });

          if (res.status === 401) {
            this.error = 'Invalid username or password.';
            return;
          }
          if (!res.ok) {
            this.error = 'Login failed (HTTP ' + res.status + ').';
            return;
          }

          var data;
          try {
            data = await res.json();
          } catch (_e) {
            data = null;
          }
          if (!data || typeof data.token !== 'string' || !data.token) {
            this.error = 'Login response was missing a session token.';
            return;
          }
          this.setToken(data.token);
          this.login.password = '';
          await this.loadDashboard();
        } catch (_err) {
          this.error = 'Network error: cannot reach go-rag daemon.';
        } finally {
          this.loading = false;
        }
      },

      /** Drop the session. Best-effort server-side revoke; always re-locks locally. */
      logout: async function () {
        try {
          await this.api('/logout', { method: 'POST' });
        } catch (_e) {
          // Network failure during logout is non-fatal — clear locally anyway.
        }
        this.clearToken();
        this.stats = {};
        this.currentView = 'dashboard';
        this.error = '';
      },

      // === Dashboard ========================================================

      /** GET /api/dashboard/stats → DashboardDTO (data-model.md). */
      loadDashboard: async function () {
        this.error = '';
        var res = await this.api('/api/dashboard/stats');
        if (!res || res.status === 401) {
          // api() already cleared the token; the gate will re-show login.
          return;
        }
        if (!res.ok) {
          this.error = 'Failed to load dashboard (HTTP ' + res.status + ').';
          return;
        }
        var data;
        try {
          data = await res.json();
        } catch (_e) {
          this.error = 'Dashboard response was not valid JSON.';
          return;
        }
        if (!data) {
          this.error = 'Dashboard response was empty.';
          return;
        }
        this.stats = data;
      },

      // === View switching ===================================================

      /** Swap the current view; refresh stats when returning to Dashboard. */
      switchView: function (view) {
        this.currentView = view;
        this.error = '';
        if (view === 'dashboard') {
          this.loadDashboard();
        }
        if (view === 'documents') {
          this.loadDocuments('');
        }
      },

      // === Documents (spec 047 US1) =======================================
      docs: [],
      docsNextToken: '',
      docsLoading: false,
      docFilterStatus: '',
      docFilterTag: '',
      docSearchQ: '',
      docSortKey: 'ingested_at',
      docSortDir: 'asc',

      /** GET /api/documents → {documents, next_page_token}. Status/tag filters
       *  go server-side (engine); sort is page-local over the cursor-paginated
       *  page (R7: corpus-wide non-date sort is deferred). */
      loadDocuments: async function (pageToken) {
        this.error = '';
        this.docsLoading = true;
        try {
          var q = '/api/documents?page_size=50';
          if (pageToken) q += '&page_token=' + encodeURIComponent(pageToken);
          if (this.docFilterStatus) q += '&status=' + encodeURIComponent(this.docFilterStatus);
          var tag = (this.docFilterTag || '').trim();
          if (tag) q += '&tag=' + encodeURIComponent(tag);
          var res = await this.api(q);
          if (!res || res.status === 401) return;
          if (!res.ok) { this.error = 'Failed to load documents (HTTP ' + res.status + ').'; return; }
          var data = await res.json();
          this.docs = data.documents || [];
          this.docsNextToken = data.next_page_token || '';
        } catch (_e) {
          this.error = 'Network error loading documents.';
        } finally {
          this.docsLoading = false;
        }
      },

      /** Cursor forward one page. */
      nextDocPage: function () {
        if (!this.docsNextToken) return;
        this.loadDocuments(this.docsNextToken);
      },

      /** Re-fetch page 1 with the current filters. */
      applyDocFilters: function () {
        this.loadDocuments('');
      },

      /** Clear filters/search and re-fetch the list. */
      clearDocFilters: function () {
        this.docFilterStatus = '';
        this.docFilterTag = '';
        this.docSearchQ = '';
        this.loadDocuments('');
      },

      /** Content-search the corpus; replaces the list with ranked matches (R2). */
      searchDocuments: async function () {
        var q = (this.docSearchQ || '').trim();
        if (!q) { this.loadDocuments(''); return; }
        this.error = '';
        this.docsLoading = true;
        try {
          var res = await this.api('/api/documents/search?q=' + encodeURIComponent(q) + '&limit=20');
          if (!res || res.status === 401) return;
          if (!res.ok) { this.error = 'Search failed (HTTP ' + res.status + ').'; return; }
          var data = await res.json();
          this.docs = data.documents || [];
          this.docsNextToken = '';
        } catch (_e) {
          this.error = 'Network error during search.';
        } finally {
          this.docsLoading = false;
        }
      },

      /** Toggle the page-local sort column/direction. */
      setDocSort: function (key) {
        if (this.docSortKey === key) {
          this.docSortDir = this.docSortDir === 'asc' ? 'desc' : 'asc';
        } else {
          this.docSortKey = key;
          this.docSortDir = 'asc';
        }
      },

      /** Page-local sort of the current page's documents (R7). */
      sortedDocs: function () {
        var key = this.docSortKey;
        var dir = this.docSortDir === 'desc' ? -1 : 1;
        return (this.docs || []).slice().sort(function (a, b) {
          var va, vb;
          if (key === 'file_size' || key === 'chunk_count') {
            va = Number(a[key]) || 0;
            vb = Number(b[key]) || 0;
          } else {
            va = String(a[key] || '').toLowerCase();
            vb = String(b[key] || '').toLowerCase();
          }
          if (va < vb) return -1 * dir;
          if (va > vb) return 1 * dir;
          return 0;
        });
      },

      /** Human-readable byte size. */
      formatBytes: function (n) {
        n = Number(n) || 0;
        if (n < 1024) return n + ' B';
        if (n < 1048576) return (n / 1024).toFixed(1) + ' KB';
        if (n < 1073741824) return (n / 1048576).toFixed(1) + ' MB';
        return (n / 1073741824).toFixed(1) + ' GB';
      },

      // === Documents detail (spec 047 US2) =======================================
      selectedDoc: null,        // documentDTO of the open document (null = list view)
      docChunks: [],
      docChunksNextToken: '',
      docChunksLoading: false,
      selectedChunk: null,      // chunkDTO currently shown with its context window
      chunkContext: null,       // chunkContextResponse for the selected chunk

      /** Open a document: fetch its detail + first chunk page. */
      openDocument: async function (id) {
        this.error = '';
        this.selectedDoc = null;
        this.docChunks = [];
        this.docChunksNextToken = '';
        this.selectedChunk = null;
        this.chunkContext = null;
        var res = await this.api('/api/documents/' + encodeURIComponent(id));
        if (!res || res.status === 401) return;
        if (res.status === 404) { this.error = 'Document not found.'; return; }
        if (!res.ok) { this.error = 'Failed to load document (HTTP ' + res.status + ').'; return; }
        this.selectedDoc = await res.json();
        await this.loadDocChunks('');
      },

      /** Load (or extend) the open document's chunk list. */
      loadDocChunks: async function (pageToken) {
        if (!this.selectedDoc) return;
        this.docChunksLoading = true;
        var id = encodeURIComponent(this.selectedDoc.id);
        var q = '/api/documents/' + id + '/chunks?page_size=20';
        if (pageToken) q += '&page_token=' + encodeURIComponent(pageToken);
        var res = await this.api(q);
        this.docChunksLoading = false;
        if (!res || res.status === 401) return;
        if (!res.ok) { this.error = 'Failed to load chunks.'; return; }
        var data = await res.json();
        if (pageToken) { this.docChunks = this.docChunks.concat(data.chunks || []); }
        else { this.docChunks = data.chunks || []; }
        this.docChunksNextToken = data.next_page_token || '';
      },

      /** Cursor forward one chunk page. */
      nextDocChunkPage: function () {
        if (!this.docChunksNextToken) return;
        this.loadDocChunks(this.docChunksNextToken);
      },

      /** Select a chunk → fetch its neighbour context window. */
      selectChunk: async function (chunkID) {
        this.error = '';
        this.selectedChunk = null;
        this.chunkContext = null;
        if (!this.selectedDoc) return;
        var id = encodeURIComponent(this.selectedDoc.id);
        var cid = encodeURIComponent(chunkID);
        var res = await this.api('/api/documents/' + id + '/chunks/' + cid + '/context?window=1');
        if (!res || res.status === 401) return;
        if (!res.ok) return;
        var data = await res.json();
        this.chunkContext = data;
        if (data && data.target_index >= 0 && data.chunks && data.chunks[data.target_index]) {
          this.selectedChunk = data.chunks[data.target_index];
        }
      },

      /** Close the detail pane; return to the list. */
      closeDocument: function () {
        this.selectedDoc = null;
        this.docChunks = [];
        this.docChunksNextToken = '';
        this.selectedChunk = null;
        this.chunkContext = null;
      },

      /** Client-side placeholder panel — mirrors templates/_placeholder.html.
       *  Returns an HTML string; the calling x-html binding injects it. */
      placeholderHtml: function (viewName, futureSpec) {
        var safeName = String(viewName).replace(/[<>&"']/g, '');
        var safeSpec = String(futureSpec).replace(/[^0-9]/g, '');
        return (
          '<div class="placeholder-title">' + safeName + '</div>' +
          '<div class="placeholder-spec">planned — spec ' + safeSpec + '</div>'
        );
      },

      // === Token storage helpers ===========================================

      setToken: function (token) {
        this.token = token;
        try {
          if (window.localStorage) {
            localStorage.setItem(GORAG_TOKEN_KEY, token);
          }
        } catch (_e) {
          // storage unavailable — in-memory only
        }
      },

      clearToken: function () {
        this.token = '';
        try {
          if (window.localStorage) {
            localStorage.removeItem(GORAG_TOKEN_KEY);
          }
        } catch (_e) {
          // storage unavailable — nothing to clear
        }
      },
    };
  }

  // Expose globally so Alpine's x-data="goragApp()" lookup can find it.
  // With both scripts defer-loaded in order (app.js before alpine.min.js),
  // this assignment runs before Alpine boots on DOMContentLoaded.
  window.goragApp = goragApp;
})();
