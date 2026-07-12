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
        if (view === 'operations') {
          this.loadBridgeOps();
        }
      },

      // === Documents (spec 047 US1) =======================================
      docs: [],
      docsNextToken: '',
      docsLoading: false,
      docFilterStatus: '',
      docFilterTag: '',
      docSearchQ: '',
      searchMode: false,
      searchResults: [],
      docSortKey: 'ingested_at',
      docSortDir: 'asc',

      // === Documents write surface (spec 050) ==============================
      // Add dialog: POST /api/documents {path, glob?}. Path-based (no upload).
      addDialog: { open: false, path: '', glob: '', loading: false },
      // Generic confirmation dialog for the destructive actions (remove,
      // reingest). Confirmation is a client-side UX gate (R7) — the server
      // executes the guarded mutation on receipt.
      confirmDialog: {
        open: false, title: '', message: '', confirmLabel: 'Confirm',
        danger: false, busy: false, action: '', targetId: '', targetLabel: '',
      },

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
        this.searchMode = false;
        this.searchResults = [];
        this.loadDocuments('');
      },

      // === Documents write actions (spec 050) ==============================
      // Add/reingest ACK fast (async-after-ACK); the pending/embedding state
      // surfaces via the doc's status badge (pending → embedded) on refresh.
      // Remove is synchronous → the row disappears on ACK.

      /** Open the Add dialog (path + optional glob). */
      openAddDialog: function () {
        this.addDialog.path = '';
        this.addDialog.glob = '';
        this.addDialog.loading = false;
        this.addDialog.open = true;
      },

      closeAddDialog: function () {
        if (this.addDialog.loading) return; // disable-on-submit (R7)
        this.addDialog.open = false;
      },

      /** Submit POST /api/documents. On 200, refresh the list (the new row shows
       *  a pending badge until embed completes) and close the dialog. */
      submitAdd: async function () {
        var path = (this.addDialog.path || '').trim();
        if (!path) { this.error = 'Path is required.'; return; }
        this.error = '';
        this.addDialog.loading = true;
        try {
          var res = await this.api('/api/documents', {
            method: 'POST',
            body: JSON.stringify({ path: path, glob: (this.addDialog.glob || '').trim() }),
          });
          if (!res || res.status === 401) return;
          if (res.status === 400) {
            var e = await res.json().catch(function () { return {}; });
            this.error = e.error || 'Invalid path.';
            return;
          }
          if (!res.ok) { this.error = 'Add failed (HTTP ' + res.status + ').'; return; }
          this.addDialog.open = false;
          await this.loadDocuments('');
        } catch (_e) {
          this.error = 'Network error during add.';
        } finally {
          this.addDialog.loading = false;
        }
      },

      /** Open the Remove confirmation for a document (index-only — source file
       *  preserved). stopPropagation so the row click doesn't also open the doc. */
      confirmRemove: function (doc, ev) {
        if (ev) ev.stopPropagation();
        this.confirmDialog = {
          open: true,
          title: 'Remove document',
          message: 'Remove "' + (doc.file_name || doc.file_path) + '" from the index? Its chunks and embeddings are deleted. The source file on disk is NOT touched (index-only — re-add the path to restore it).',
          confirmLabel: 'Remove',
          danger: true,
          busy: false,
          action: 'remove',
          targetId: doc.id,
          targetLabel: doc.file_name || doc.file_path,
        };
      },

      /** Open the Reingest confirmation for a document. */
      confirmReingest: function (doc, ev) {
        if (ev) ev.stopPropagation();
        this.confirmDialog = {
          open: true,
          title: 'Reingest document',
          message: 'Re-derive the chunks and embeddings for "' + (doc.file_name || doc.file_path) + '" from its source path? Dedup is bypassed (the current reader/embedder applies).',
          confirmLabel: 'Reingest',
          danger: false,
          busy: false,
          action: 'reingest',
          targetId: doc.id,
          targetLabel: doc.file_name || doc.file_path,
        };
      },

      closeConfirm: function () {
        if (this.confirmDialog.busy) return; // disable-on-submit (R7)
        this.confirmDialog.open = false;
      },

      /** Execute the confirmed action (remove or reingest). Never proceeds
       *  without an explicit confirm — the dialog is the gate. */
      doConfirm: async function () {
        var cd = this.confirmDialog;
        var id = cd.targetId;
        if (!id || !cd.open) return;
        cd.busy = true;
        this.error = '';
        try {
          if (cd.action === 'remove') {
            var del = await this.api('/api/documents/' + encodeURIComponent(id), { method: 'DELETE' });
            if (!del || del.status === 401) return;
            if (del.status === 404) { this.error = 'Document not found (already removed?).'; }
            else if (!del.ok && del.status !== 204) { this.error = 'Remove failed (HTTP ' + del.status + ').'; }
          } else if (cd.action === 'reingest') {
            var re = await this.api('/api/documents/' + encodeURIComponent(id) + '/reingest', { method: 'POST' });
            if (!re || re.status === 401) return;
            if (re.status === 404) {
              var e = await re.json().catch(function () { return {}; });
              this.error = e.error === 'source not found' ? 'Source file no longer exists.' : 'Document not found.';
            } else if (!re.ok) {
              this.error = 'Reingest failed (HTTP ' + re.status + ').';
            }
          }
          this.confirmDialog.open = false;
          // If the open document was removed, drop back to the list before refresh.
          if (cd.action === 'remove' && this.selectedDoc && this.selectedDoc.id === id) {
            this.closeDocument();
          }
          await this.loadDocuments('');
        } catch (_e) {
          this.error = 'Network error during ' + cd.action + '.';
        } finally {
          cd.busy = false;
        }
      },

      /** Content-search the corpus; shows ranked results with the found-text snippet (R2). */
      searchDocuments: async function () {
        var q = (this.docSearchQ || '').trim();
        if (!q) { this.searchMode = false; this.loadDocuments(''); return; }
        this.error = '';
        this.docsLoading = true;
        try {
          var res = await this.api('/api/documents/search?q=' + encodeURIComponent(q) + '&limit=20');
          if (!res || res.status === 401) return;
          if (!res.ok) { this.error = 'Search failed (HTTP ' + res.status + ').'; return; }
          var data = await res.json();
          this.searchResults = data.results || [];
          this.searchMode = true;
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

      // === Query (spec 048) ============================================
      // Read-only retrieval view: POST /api/query → Engine.Query in-process.
      // Explicit submit only (R9) — controls never auto-fire. include_quarantined
      // resets to false on each new query (R8: quarantine-by-default).
      queryInput: '',
      queryForm: {
        k: 5,
        mode: 'hybrid',
        no_rerank: false,
        threshold: 0,
        rrf_k: 0,
        pool_size: 0,
        source: '',
        type: '',
        tags: '', // comma-separated in the UI; split + trimmed on submit
        context_window: 0,
        no_cache: false,
        include_quarantined: false,
        dedup: false,
      },
      queryLoading: false,
      queryResult: null, // queryResponseDTO (hits + transparency) or null
      queryError: '',    // operator-facing detail for the current state
      queryErrorKind: '', // '' | 'empty' | 'noresults' | 'embedder' | 'mismatch' | 'unauth' | 'network'
      selectedHit: null,  // queryHitDTO open in the detail pane (client-side, no round-trip — R7)
      queryRequestedK: 5, // the (clamped) k actually sent, for the adaptive-depth note (R6)

      /** clampTopK keeps k in [1,50] (R7: default 5, sane ceiling). */
      clampTopK: function (n) {
        n = Number(n) || 5;
        if (n < 1) n = 1;
        if (n > 50) n = 50;
        return n;
      },

      /** Build the /api/query request body from the form state. */
      queryRequestBody: function () {
        var k = this.clampTopK(this.queryForm.k);
        var tags = (this.queryForm.tags || '')
          .split(',')
          .map(function (t) { return t.trim(); })
          .filter(function (t) { return t.length > 0; });
        // R8: capture the per-query quarantine opt-in, then reset the toggle so
        // the resting state is always safe (quarantine-by-default).
        var includeQuarantined = !!this.queryForm.include_quarantined;
        this.queryForm.include_quarantined = false;
        return {
          query: (this.queryInput || '').trim(),
          k: k,
          mode: this.queryForm.mode || 'hybrid',
          no_rerank: !!this.queryForm.no_rerank,
          threshold: Number(this.queryForm.threshold) || 0,
          rrf_k: Number(this.queryForm.rrf_k) || 0,
          pool_size: Number(this.queryForm.pool_size) || 0,
          source: (this.queryForm.source || '').trim(),
          type: (this.queryForm.type || '').trim(),
          tags: tags,
          context_window: Number(this.queryForm.context_window) || 0,
          no_cache: !!this.queryForm.no_cache,
          include_quarantined: includeQuarantined,
          dedup: !!this.queryForm.dedup,
        };
      },

      /** Submit the query (explicit — Enter or Search button). R9: never auto-fires. */
      runQuery: async function () {
        var body = this.queryRequestBody();
        this.queryRequestedK = body.k;
        // Client-side guard: empty/whitespace query → empty state, not a request (R11).
        if (!body.query) {
          this.queryResult = null;
          this.selectedHit = null;
          this.queryError = '';
          this.queryErrorKind = 'empty';
          return;
        }
        this.queryError = '';
        this.queryErrorKind = '';
        this.queryLoading = true;
        this.selectedHit = null;
        try {
          var res = await this.api('/api/query', {
            method: 'POST',
            body: JSON.stringify(body),
          });
          if (!res) return;
          if (res.status === 401) {
            // api() already cleared the token → the gate re-locks to login.
            this.queryErrorKind = 'unauth';
            this.queryError = 'Session expired.';
            this.queryResult = null;
            return;
          }
          if (res.status === 503) {
            // Embedder unreachable (semantic/vector needs local Ollama) — suggest keyword.
            this.queryErrorKind = 'embedder';
            this.queryError = await this.readErrDetail(res);
            this.queryResult = null;
            return;
          }
          if (res.status === 400) {
            var msg = await this.readErrMsg(res);
            if (msg === 'embedding mismatch') {
              this.queryErrorKind = 'mismatch';
              this.queryError = await this.readErrDetail(res);
            } else {
              // 'empty query' (shouldn't happen — client guards) or 'invalid mode'.
              this.queryErrorKind = 'network';
              this.queryError = msg || ('Bad request (HTTP 400).');
            }
            this.queryResult = null;
            return;
          }
          if (!res.ok) {
            this.queryErrorKind = 'network';
            this.queryError = 'Query failed (HTTP ' + res.status + ').';
            this.queryResult = null;
            return;
          }
          var data;
          try {
            data = await res.json();
          } catch (_e) {
            this.queryErrorKind = 'network';
            this.queryError = 'Query response was not valid JSON.';
            this.queryResult = null;
            return;
          }
          this.queryResult = data;
          // No-results state: distinguish "corpus empty" from "nothing matched".
          if (!data || !data.hits || data.hits.length === 0) {
            this.queryErrorKind = 'noresults';
          }
        } catch (_e) {
          this.queryErrorKind = 'network';
          this.queryError = 'Network error running the query.';
          this.queryResult = null;
        } finally {
          this.queryLoading = false;
        }
      },

      /** readErrMsg pulls {"error": "..."} from a JSON error response (best-effort). */
      readErrMsg: async function (res) {
        try {
          var m = await res.clone().json();
          return m && typeof m.error === 'string' ? m.error : '';
        } catch (_e) {
          return '';
        }
      },

      /** readErrDetail pulls {"detail": "..."} (or {"error":...}) for guidance. */
      readErrDetail: async function (res) {
        try {
          var m = await res.clone().json();
          if (!m) return '';
          if (typeof m.detail === 'string') return m.detail;
          if (typeof m.error === 'string') return m.error;
          return '';
        } catch (_e) {
          return '';
        }
      },

      /** Open a hit's detail pane (client-side — the payload already carries
       *  full text + context + provenance, so no second round-trip — R7). */
      selectHit: function (hit) {
        this.selectedHit = hit;
      },

      /** Close the detail pane; return to the result list. */
      closeHit: function () {
        this.selectedHit = null;
      },

      /** Format a score to 3 decimals for display. */
      fmtScore: function (s) {
        return (Number(s) || 0).toFixed(3);
      },

      // === Operations (spec 049) ============================================
      // Read-only operational-health view: GET /api/bridge-ops/stats +
      // /api/bridge-ops/activity, both in-process engine reads (the UI is a 4th
      // adapter, not a REST proxy — R1). Manual refresh only (R8 — no auto-poll
      // or SSE). Empty / embedder-down / scan-driven states degrade plainly,
      // never a silent crash (US4).
      bridgeStats: null,        // bridgeOpsStatsDTO or null (until first load)
      bridgeActivity: [],       // activityEventDTO[] (most-recent-first)
      bridgeLoading: false,
      bridgeError: '',
      bridgeActivityType: 'ingest',
      bridgeActivityTail: 20,

      /** Fetch stats + activity together (both refresh on view-entry + click). */
      loadBridgeOps: async function () {
        this.bridgeError = '';
        this.bridgeLoading = true;
        await Promise.all([this.loadBridgeStats(), this.loadBridgeActivity()]);
        this.bridgeLoading = false;
      },

      /** Refresh-button alias — reload both payloads. */
      refreshBridgeOps: function () {
        this.loadBridgeOps();
      },

      /** GET /api/bridge-ops/stats → operational projection of engine.Status(). */
      loadBridgeStats: async function () {
        var res = await this.api('/api/bridge-ops/stats');
        if (!res || res.status === 401) return; // api() re-locks the gate
        if (!res.ok) {
          this.bridgeError = 'Failed to load bridge ops (HTTP ' + res.status + ').';
          return;
        }
        try {
          this.bridgeStats = await res.json();
        } catch (_e) {
          this.bridgeError = 'Bridge ops response was not valid JSON.';
        }
      },

      /** GET /api/bridge-ops/activity?tail=N&type=T → bounded recent feed. */
      loadBridgeActivity: async function () {
        var q = '/api/bridge-ops/activity?tail=' + encodeURIComponent(this.bridgeActivityTail) +
          '&type=' + encodeURIComponent(this.bridgeActivityType);
        var res = await this.api(q);
        if (!res || res.status === 401) return;
        if (!res.ok) {
          // 400 invalid-type is guarded client-side by the select; any other
          // non-ok degrades to empty + a soft error (never a crash).
          this.bridgeActivity = [];
          this.bridgeError = 'Failed to load activity (HTTP ' + res.status + ').';
          return;
        }
        try {
          var data = await res.json();
          this.bridgeActivity = (data && data.events) || [];
        } catch (_e) {
          this.bridgeActivity = [];
        }
      },

      /** Broaden the activity type filter, then reload the feed (US2 control). */
      changeActivityType: function (t) {
        if (t !== 'ingest' && t !== 'query' && t !== 'auth-fail') return;
        this.bridgeActivityType = t;
        this.loadBridgeActivity();
      },

      /** Adjust the tail size (clamped client-side; the server clamps too — R4). */
      changeActivityTail: function (raw) {
        var n = Number(raw) || 20;
        if (n < 1) n = 1;
        if (n > 100) n = 100;
        this.bridgeActivityTail = n;
        this.loadBridgeActivity();
      },

      /** One-line cache summary for the caches subsystem tile. */
      cacheSummary: function (c) {
        if (!c) return '—';
        if (!c.enabled) return 'off';
        return c.size + '/' + c.capacity + ' (hits ' + c.hits + ', miss ' + c.misses + ')';
      },

      /** Trim an RFC3339 timestamp to a readable UTC form for the feed/tiles. */
      fmtTS: function (ts) {
        if (!ts) return '';
        return ts.replace('T', ' ').replace(/Z$/, '');
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
