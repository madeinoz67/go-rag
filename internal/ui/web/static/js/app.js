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
  var GORAG_VAULT_KEY = 'goragVault';
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

      // === Vault selection ==================================================
      // Active vault (default "default"). Sent as X-Go-Rag-Vault on every
      // /api/* fetch (see api()). The picker is populated from the server by
      // loadVaults() (spec 051 Vaults view); until the first load it offers
      // "default".
      vault: 'default',
      vaults: ['default'],
      // Vaults view (spec 051) — list + load state, populated by loadVaults.
      vaultsList: [],
      vaultsLoading: false,
      vaultsError: '',
      // Create/rename dialog (both need a name input; clear/delete reuse the
      // shared confirmDialog). mode = 'create' | 'rename'.
      vaultNameDialog: { open: false, mode: 'create', title: '', label: '', value: '', busy: false, action: '', targetVault: '' },
      vaultSortKey: 'name',   // page-local sort column (name | documents)
      vaultSortDir: 'asc',
      vaultChecked: {}, // selected vault name → true (bulk-delete selection; default excluded)

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
        // Restore the last-selected vault (best-effort; defaults to "default").
        try {
          if (window.localStorage) {
            var v = localStorage.getItem(GORAG_VAULT_KEY);
            if (v) this.vault = v;
          }
        } catch (_e) {
          // storage unavailable — keep the in-memory default
        }
        if (this.isAuthed()) {
          this.loadDashboard();
          this.loadVaults();
        }
      },

      /** Persist + activate a vault selection, then refresh the current view. */
      switchVault: function (v) {
        v = String(v || '').trim() || 'default';
        if (v === this.vault) return;
        this.vault = v;
        try {
          if (window.localStorage) {
            localStorage.setItem(GORAG_VAULT_KEY, v);
          }
        } catch (_e) {
          // storage unavailable — in-memory only
        }
        // Drop any cached per-vault state, then reload the active view.
        this.stats = {};
        this.docs = [];
        this.docsNextToken = '';
        this.searchMode = false;
        this.searchResults = [];
        this.queryResult = null;
        this.selectedHit = null;
        this.selectedDoc = null;
        this.bridgeStats = null;
        this.bridgeActivity = [];
        this.quarChunks = [];
        this.quarSelected = null;
        this.quarChecked = {};
        this.vaultsList = [];
        this.vaultChecked = {};
        this.switchView(this.currentView);
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
        // Pin the active vault on every API call so the server handler targets
        // the operator's chosen vault (vaultFromRequest reads this header).
        if (this.vault) {
          headers['X-Go-Rag-Vault'] = this.vault;
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
        if (view === 'quarantine') {
          this.loadQuarantine();
        }
        if (view === 'vaults') {
          this.loadVaults();
        }
        if (view === 'observability') {
          this.loadObservability();
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

      /** Execute the confirmed action (remove, reingest, or a quarantine op). Never
       *  proceeds without an explicit confirm — the dialog is the gate. */
      doConfirm: async function () {
        var cd = this.confirmDialog;
        if (!cd.open) return;
        cd.busy = true;
        this.error = '';
        try {
          // Vault-wide rescan has no target id.
          if (cd.action === 'quar-rescan') {
            var rsc = await this.api('/api/quarantine/rescan', { method: 'POST' });
            if (!rsc || rsc.status === 401) return;
            if (!rsc.ok && rsc.status !== 204) { this.error = 'Rescan failed (HTTP ' + rsc.status + ').'; }
            this.quarScanning = false;
          } else if (cd.action === 'quar-bulk-release') {
            // Fan out the existing per-chunk release (no new endpoint — the bulk
            // action is a client-side composition of ReleaseChunk, already on
            // every transport). 204 = released; any 401 re-locks the gate.
            var ids = Object.keys(this.quarChecked || {});
            if (ids.length === 0) { this.confirmDialog.open = false; return; }
            var bulkFailed = 0;
            await Promise.all(ids.map(async (id) => {
              if (!this.token) return; // session expired — stop fanning out
              try {
                var r = await this.api('/api/quarantine/' + encodeURIComponent(id) + '/release', { method: 'POST' });
                if (!this.token) return; // api() cleared the token on a 401
                if (r && r.status !== 204) bulkFailed++;
              } catch (_e) { bulkFailed++; }
            }));
            if (!this.token) return; // gate re-locked by a 401 mid-batch
            if (bulkFailed > 0) { this.error = bulkFailed + ' chunk(s) could not be released — release the rest individually.'; }
            this.quarChecked = {};
          } else if (cd.action === 'vault-bulk-delete') {
            // Irreversible — defense-in-depth: the button is disabled until
            // DELETE is typed, but re-check the phrase here regardless.
            if (!this.confirmArmed()) return;
            // Fan out DELETE over each selected vault (default is excluded from
            // selection; safety-skip here too). Mirrors quar-bulk-release.
            var vnames = Object.keys(this.vaultChecked || {});
            if (vnames.length === 0) { this.confirmDialog.open = false; return; }
            var vfail = 0;
            await Promise.all(vnames.map(async (nm) => {
              if (nm === 'default') return; // never delete default
              if (!this.token) return;
              try {
                var r = await this.api('/api/vaults/' + encodeURIComponent(nm), { method: 'DELETE' });
                if (!this.token) return; // api() cleared the token on a 401
                if (r && r.status !== 204) vfail++;
              } catch (_e) { vfail++; }
            }));
            if (!this.token) return;
            if (vfail > 0) { this.error = vfail + ' vault(s) could not be deleted — delete the rest individually.'; }
            this.vaultChecked = {};
          } else {
            var id = cd.targetId;
            if (!id) { this.confirmDialog.open = false; return; }
            if (cd.action === 'remove') {
              var del = await this.api('/api/documents/' + encodeURIComponent(id), { method: 'DELETE' });
              if (!del || del.status === 401) return;
              if (del.status === 404) { this.error = 'Document not found (already removed?).'; }
              else if (!del.ok && del.status !== 204) { this.error = 'Remove failed (HTTP ' + del.status + ').'; }
            } else if (cd.action === 'reingest') {
              var re = await this.api('/api/documents/' + encodeURIComponent(id) + '/reingest', { method: 'POST' });
              if (!re || re.status === 401) return;
              if (re.status === 404) {
                var reErr = await re.json().catch(function () { return {}; });
                this.error = reErr.error === 'source not found' ? 'Source file no longer exists.' : 'Document not found.';
              } else if (!re.ok) {
                this.error = 'Reingest failed (HTTP ' + re.status + ').';
              }
            } else if (cd.action === 'quar-release') {
              var rel = await this.api('/api/quarantine/' + encodeURIComponent(id) + '/release', { method: 'POST' });
              if (!rel || rel.status === 401) return;
              if (rel.status === 404) { this.error = 'Chunk not found (already released?).'; }
              else if (!rel.ok && rel.status !== 204) { this.error = 'Release failed (HTTP ' + rel.status + ').'; }
            } else if (cd.action === 'quar-reset') {
              var rst = await this.api('/api/quarantine/' + encodeURIComponent(id) + '/reset', { method: 'POST' });
              if (!rst || rst.status === 401) return;
              if (rst.status === 404) { this.error = 'Chunk not found.'; }
              else if (!rst.ok && rst.status !== 204) { this.error = 'Reset failed (HTTP ' + rst.status + ').'; }
            } else if (cd.action === 'vault-clear') {
              var vclr = await this.api('/api/vaults/' + encodeURIComponent(id) + '/clear', { method: 'POST' });
              if (!vclr || vclr.status === 401) return;
              if (!vclr.ok && vclr.status !== 204) { this.error = 'Clear failed (HTTP ' + vclr.status + ').'; }
            } else if (cd.action === 'vault-delete') {
              var vdel = await this.api('/api/vaults/' + encodeURIComponent(id), { method: 'DELETE' });
              if (!vdel || vdel.status === 401) return;
              if (vdel.status === 400) { this.error = 'The default vault cannot be deleted.'; }
              else if (!vdel.ok && vdel.status !== 204) { this.error = 'Delete failed (HTTP ' + vdel.status + ').'; }
              else if (this.vault === id) {
                // Deleted the active vault — fall back to default + persist.
                this.vault = 'default';
                try { if (window.localStorage) { localStorage.setItem(GORAG_VAULT_KEY, 'default'); } } catch (_e) {}
              }
            }
          }
          this.confirmDialog.open = false;
          // Drop a detail pane if its object was removed/released before refresh.
          if (cd.action === 'remove' && this.selectedDoc && this.selectedDoc.id === id) {
            this.closeDocument();
          }
          if (cd.action === 'quar-release' && this.quarSelected && this.quarSelected.chunk_id === id) {
            this.closeQuarChunk();
          }
          // Refresh the owning view's data.
          if (cd.action === 'quar-release' || cd.action === 'quar-reset' || cd.action === 'quar-rescan' || cd.action === 'quar-bulk-release') {
            await this.loadQuarantine();
          } else if (cd.action === 'vault-clear' || cd.action === 'vault-delete' || cd.action === 'vault-bulk-delete') {
            await this.loadVaults();
          } else {
            await this.loadDocuments('');
          }
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

      // === Quarantine (spec 053) ============================================
      // The dedicated quarantine-management surface: list flagged chunks, inspect
      // the verdict (per-signal scores + matched-phrase highlights), and
      // release/reset/rescan with confirmation. Reuses the shared confirmDialog
      // (doConfirm dispatches quar-release/quar-reset/quar-rescan). Read + confirmed-
      // write; the active vault flows via the X-Go-Rag-Vault header api() pins on
      // every call, so the list is isolated per vault (FR-005).
      quarChunks: [],
      quarLoading: false,
      quarError: '',
      quarSelected: null, // quarantineDetailDTO open in the detail pane (null = list)
      // === Observability (spec 054) ======================================
      // Telemetry (US1) + audit-log browser (US2) + posture (US3). Both surfaces
      // are process-wide (one daemon, all vaults). Audit rows carry the query
      // hash only — never the raw query (FR-003).
      obsMetrics: null,      // telemetryResponse (US1)
      obsLoading: false,
      obsError: '',
      obsOpSortKey: 'count', // telemetry table sort (op|count|errors|p50|p95|p99)
      obsOpSortDir: 'desc',
      obsAudit: null,        // auditPageResponse (US2)
      obsAuditType: '',      // '' | query | ingest | auth-fail
      obsAuditWindow: '',    // '' | 1h | 24h | 168h (Go duration → ?since=)
      obsSortKey: 'ts',      // audit table sort
      obsSortDir: 'desc',
      obsAutoPoll: false,    // telemetry auto-refresh toggle (spec 054 polish)
      obsPollTimer: null,    // setInterval handle for auto-poll

      /** Load both panels (called on view entry + Refresh). */
      loadObservability: async function () {
        this.obsError = '';
        this.obsLoading = true;
        try {
          await Promise.all([this.loadObsTelemetry(), this.loadObsAudit()]);
        } finally {
          this.obsLoading = false;
        }
      },

      /** Toggle telemetry auto-refresh (10s); only fetches while the Observability
          view is active so it stays cold on other views. */
      toggleObsAutoPoll: function () {
        if (this.obsAutoPoll) {
          if (this.obsPollTimer) { clearInterval(this.obsPollTimer); }
          var self = this;
          this.obsPollTimer = setInterval(function () {
            if (self.currentView === 'observability') { self.loadObsTelemetry(); }
          }, 10000);
        } else if (this.obsPollTimer) {
          clearInterval(this.obsPollTimer);
          this.obsPollTimer = null;
        }
      },

      /** GET /api/observability/metrics → telemetryResponse. Always 200. */
      loadObsTelemetry: async function () {
        try {
          var res = await this.api('/api/observability/metrics');
          if (!res || res.status === 401) return;
          if (!res.ok) { this.obsError = 'Failed to load telemetry (HTTP ' + res.status + ').'; return; }
          this.obsMetrics = await res.json();
        } catch (_e) {
          this.obsError = 'Network error loading telemetry.';
        }
      },

      /** GET /api/observability/audit?type=… → auditPageResponse. */
      loadObsAudit: async function () {
        try {
          var url = '/api/observability/audit?limit=50';
          if (this.obsAuditType) { url += '&type=' + encodeURIComponent(this.obsAuditType); }
          if (this.obsAuditWindow) { url += '&since=' + encodeURIComponent(this.obsAuditWindow); }
          var res = await this.api(url);
          if (!res || res.status === 401) return;
          if (!res.ok) { this.obsError = 'Failed to load audit log (HTTP ' + res.status + ').'; return; }
          this.obsAudit = await res.json();
        } catch (_e) {
          this.obsError = 'Network error loading audit log.';
        }
      },

      setObsOpSort: function (key) {
        if (this.obsOpSortKey === key) { this.obsOpSortDir = this.obsOpSortDir === 'asc' ? 'desc' : 'asc'; }
        else { this.obsOpSortKey = key; this.obsOpSortDir = key === 'op' ? 'asc' : 'desc'; }
      },
      sortedObsOps: function () {
        var rows = (this.obsMetrics && this.obsMetrics.operations) ? this.obsMetrics.operations.slice() : [];
        var key = this.obsOpSortKey, dir = this.obsOpSortDir === 'asc' ? 1 : -1;
        rows.sort(function (a, b) {
          if (key === 'op') { return a.op < b.op ? -dir : a.op > b.op ? dir : 0; }
          return (a[key] - b[key]) * dir;
        });
        return rows;
      },

      setObsSort: function (key) {
        if (this.obsSortKey === key) { this.obsSortDir = this.obsSortDir === 'asc' ? 'desc' : 'asc'; }
        else { this.obsSortKey = key; this.obsSortDir = 'desc'; }
      },
      sortedObsAudit: function () {
        var rows = (this.obsAudit && this.obsAudit.events) ? this.obsAudit.events.slice() : [];
        var key = this.obsSortKey, dir = this.obsSortDir === 'asc' ? 1 : -1;
        rows.sort(function (a, b) {
          if (key === 'type') { return a.type < b.type ? -dir : a.type > b.type ? dir : 0; }
          return (a.timestamp < b.timestamp ? -1 : a.timestamp > b.timestamp ? 1 : 0) * dir;
        });
        return rows;
      },

      /** Format a latency percentile in ms; -1 (insufficient samples) → —. */
      obsFmtSec: function (s) {
        if (s == null || s < 0) return '—';
        return (s * 1000).toFixed(0) + ' ms';
      },
      obsFmtRate: function (r) {
        if (r == null) return '—';
        return (r * 100).toFixed(1) + '%';
      },
      obsFmtBytes: function (b) {
        if (b == null) return '—';
        if (b >= 1048576) return (b / 1048576).toFixed(1) + ' MiB';
        if (b >= 1024) return (b / 1024).toFixed(0) + ' KiB';
        return b + ' B';
      },
      obsFmtTs: function (ts) {
        if (!ts) return '';
        return ts.replace('T', ' ').replace(/\.\d+Z$/, 'Z');
      },
      obsTypeBadge: function (t) {
        return t === 'auth-fail' ? 'badge-warn' : '';
      },
      /** One-line, plaintext (x-text) summary per audit event. The DTO carries
          no query plaintext by construction (FR-003) — renders hash/counts only. */
      obsAuditSummary: function (e) {
        if (e.type === 'query') {
          return 'mode=' + (e.mode || '?') + ' k=' + (e.k || 0) + ' hits=' + (e.hits || 0) +
            ' status=' + (e.status || '?') + ' · hash=' + this.obsShortHash(e.query_hash);
        }
        if (e.type === 'ingest') {
          return (e.op || 'ingest') + ' ' + (e.path || '') + ' · new=' + (e.new || 0) +
            ' skipped=' + (e.skipped || 0) + ' errors=' + (e.errors || 0);
        }
        if (e.type === 'auth-fail') {
          return (e.transport || '?') + (e.detail ? ': ' + e.detail : '');
        }
        return e.type;
      },
      obsShortHash: function (h) {
        if (!h) return '—';
        return h.length > 12 ? h.slice(0, 12) + '…' : h;
      },

      quarScanning: false, // vault rescan in progress (UI hint until refresh)
      quarSortKey: 'score', // page-local sort column (document | level | score)
      quarSortDir: 'desc', // high-risk first by default
      quarChecked: {}, // selected chunk_id → true (bulk-release selection)

      /** GET /api/quarantine/list → {chunks, count}. Always 200 (empty = healthy). */
      loadQuarantine: async function () {
        this.quarError = '';
        this.quarLoading = true;
        try {
          var res = await this.api('/api/quarantine/list');
          if (!res || res.status === 401) return;
          if (!res.ok) { this.quarError = 'Failed to load quarantine (HTTP ' + res.status + ').'; return; }
          var data = await res.json();
          this.quarChunks = (data && data.chunks) || [];
          // Prune the bulk selection to chunks still listed (released ones are gone).
          var present = {};
          this.quarChunks.forEach(function (c) { present[c.chunk_id] = true; });
          var kept = {};
          Object.keys(this.quarChecked || {}).forEach(function (cid) {
            if (present[cid]) { kept[cid] = true; }
          });
          this.quarChecked = kept;
        } catch (_e) {
          this.quarError = 'Network error loading quarantine.';
        } finally {
          this.quarLoading = false;
        }
      },

      /** Open a flagged chunk's detail: full text + verdict (US2). */
      openQuarChunk: async function (chunkID) {
        this.quarError = '';
        this.quarSelected = null;
        var res = await this.api('/api/quarantine/' + encodeURIComponent(chunkID) + '/detail');
        if (!res || res.status === 401) return;
        if (res.status === 404) { this.quarError = 'Chunk not found (released?).'; return; }
        if (!res.ok) { this.quarError = 'Failed to load chunk detail (HTTP ' + res.status + ').'; return; }
        this.quarSelected = await res.json();
      },

      /** Close the detail pane; return to the list. */
      closeQuarChunk: function () {
        this.quarSelected = null;
      },

      /** Confirm releasing a false positive (permanent override — sticky across
       *  rescans; the chunk re-enters default retrieval). */
      confirmQuarRelease: function (chunk, ev) {
        if (ev) ev.stopPropagation();
        this.confirmDialog = {
          open: true,
          title: 'Release flagged chunk',
          message: 'Release this chunk back into the default query pool? This is a permanent false-positive override — it stays retrievable even after a rescan. The chunk text and its score are preserved.',
          confirmLabel: 'Release',
          danger: false,
          busy: false,
          action: 'quar-release',
          targetId: chunk.chunk_id,
          targetLabel: chunk.preview || '',
        };
      },

      /** Confirm a reset (force re-scan — may restore quarantine). */
      confirmQuarReset: function (chunk, ev) {
        if (ev) ev.stopPropagation();
        this.confirmDialog = {
          open: true,
          title: 'Reset chunk verdict',
          message: 'Force a re-scan of this chunk? Its level is recomputed from the stored score under the current thresholds — it may be re-quarantined. Use this to undo a release.',
          confirmLabel: 'Reset',
          danger: false,
          busy: false,
          action: 'quar-reset',
          targetId: chunk.chunk_id,
          targetLabel: chunk.preview || '',
        };
      },

      /** Confirm a vault-wide rescan (re-score every chunk; idempotent). */
      confirmQuarRescan: function () {
        this.confirmDialog = {
          open: true,
          title: 'Rescan vault',
          message: 'Re-score every chunk in this vault under the current detector and thresholds? Unchanged verdicts are a no-op. The list refreshes when complete.',
          confirmLabel: 'Rescan',
          danger: false,
          busy: false,
          action: 'quar-rescan',
          targetId: '',
          targetLabel: '',
        };
      },

      /** Number of chunks currently selected for bulk release. */
      quarCheckedCount: function () {
        return Object.keys(this.quarChecked || {}).length;
      },

      /** True when every listed chunk is selected (drives the select-all box). */
      quarAllChecked: function () {
        var list = this.quarChunks || [];
        if (list.length === 0) return false;
        var sel = this.quarChecked || {};
        for (var i = 0; i < list.length; i++) {
          if (!sel[list[i].chunk_id]) return false;
        }
        return true;
      },

      /** Toggle one chunk's selection (whole-object reassign so Alpine reactivity
       *  fires reliably). Callers @click.stop so the row's open-detail doesn't. */
      toggleQuarCheck: function (chunkID) {
        var next = Object.assign({}, this.quarChecked || {});
        if (next[chunkID]) { delete next[chunkID]; } else { next[chunkID] = true; }
        this.quarChecked = next;
      },

      /** Select every listed chunk, or clear if all are already selected. */
      toggleQuarCheckAll: function () {
        if (this.quarAllChecked()) {
          this.quarChecked = {};
          return;
        }
        var next = {};
        (this.quarChunks || []).forEach(function (c) { next[c.chunk_id] = true; });
        this.quarChecked = next;
      },

      /** Confirm releasing every selected chunk at once (US3 bulk). */
      confirmQuarBulkRelease: function () {
        var n = this.quarCheckedCount();
        if (n === 0) return;
        this.confirmDialog = {
          open: true,
          title: 'Release ' + n + ' flagged chunk' + (n === 1 ? '' : 's'),
          message: 'Release ' + n + ' selected chunk' + (n === 1 ? '' : 's') + ' back into the default query pool? Each is a permanent false-positive override — they stay retrievable even after a rescan. Chunk text and scores are preserved.',
          confirmLabel: 'Release ' + n,
          danger: false,
          busy: false,
          action: 'quar-bulk-release',
          targetId: '',
          targetLabel: '',
        };
      },

      /** Render the chunk content with every signal's triggers highlighted in its
       *  own colour: instruction phrases (purple), repetition tokens (amber),
       *  stuffing tokens (red). Position-based + overlap-aware (an instruction
       *  phrase absorbs the token-matches inside it; no nested marks); word
       *  boundaries on token matches so "the" won't light up inside "there";
       *  HTML-escaped so chunk text can never inject markup. Safe under x-html.
       *
       *  matched_phrases carries only instruction hits; repetition_matches +
       *  stuffing_matches (computed by the detail endpoint from the chunk
       *  content via the poison detector's term-extraction) give the other two
       *  signals their own highlights. */
      highlightVerdict: function (sel) {
        if (!sel) return '';
        var s = String(sel.content || '');
        function esc(t) {
          return String(t).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
        }
        var spans = [];
        function addAll(list, cls, word) {
          (list || []).forEach(function (p) {
            var term = String(p || '').trim();
            if (!term) return;
            var lower = s.toLowerCase();
            var needle = term.toLowerCase();
            var from = 0;
            while (from <= lower.length) {
              var idx = lower.indexOf(needle, from);
              if (idx < 0) break;
              if (word) {
                var before = idx > 0 ? s[idx - 1] : ' ';
                var afterC = idx + needle.length < s.length ? s[idx + needle.length] : ' ';
                if (/[a-z0-9]/i.test(before) || /[a-z0-9]/i.test(afterC)) { from = idx + 1; continue; }
              }
              spans.push({ start: idx, end: idx + needle.length, cls: cls });
              from = idx + needle.length;
            }
          });
        }
        // Instruction phrases first (most specific), then stuffing (dominant
        // token), then repetition. Priority resolves same-start overlaps.
        addAll(sel.verdict && sel.verdict.matched_phrases, 'instr', false);
        addAll(sel.stuffing_matches, 'stuff', true);
        addAll(sel.repetition_matches, 'rep', true);
        var pri = { instr: 0, stuff: 1, rep: 2 };
        spans.sort(function (a, b) {
          if (a.start !== b.start) return a.start - b.start;
          return pri[a.cls] - pri[b.cls];
        });
        // First-wins: a span overlapping an already-kept span is dropped (so a
        // token inside an instruction phrase isn't double-wrapped).
        var kept = [];
        var lastEnd = -1;
        spans.forEach(function (sp) {
          if (sp.start >= lastEnd) { kept.push(sp); lastEnd = sp.end; }
        });
        var out = '';
        var pos = 0;
        kept.forEach(function (sp) {
          out += esc(s.slice(pos, sp.start));
          out += '<mark class="poison-mark ' + sp.cls + '">' + esc(s.slice(sp.start, sp.end)) + '</mark>';
          pos = sp.end;
        });
        out += esc(s.slice(pos));
        return out;
      },

      /** Badge class for a poison level (quarantine=red, suspicious=amber, released=green). */
      quarLevelBadge: function (level) {
        if (level === 'quarantine') return 'badge-danger';
        if (level === 'suspicious') return 'badge-warn';
        if (level === 'released') return 'badge-success';
        return '';
      },

      /** Toggle the page-local sort column/direction (mirrors setDocSort). */
      setQuarSort: function (key) {
        if (this.quarSortKey === key) {
          this.quarSortDir = this.quarSortDir === 'asc' ? 'desc' : 'asc';
        } else {
          this.quarSortKey = key;
          // Risk-shaped columns default high→low; document defaults A→Z.
          this.quarSortDir = (key === 'score' || key === 'level') ? 'desc' : 'asc';
        }
      },

      /** Page-local sort of the flagged chunks (mirrors sortedDocs). Severity rank
       *  is closure-local so the sort comparator (a plain function) can reach it. */
      sortedQuar: function () {
        var key = this.quarSortKey;
        var dir = this.quarSortDir === 'desc' ? -1 : 1;
        function rank(level) {
          if (level === 'quarantine') return 3;
          if (level === 'suspicious') return 2;
          if (level === 'released') return 1;
          return 0;
        }
        return (this.quarChunks || []).slice().sort(function (a, b) {
          var va, vb;
          if (key === 'score') {
            va = Number((a.verdict && a.verdict.score) || 0);
            vb = Number((b.verdict && b.verdict.score) || 0);
          } else if (key === 'level') {
            va = rank((a.verdict && a.verdict.level) || '');
            vb = rank((b.verdict && b.verdict.level) || '');
          } else { // document
            va = String(a.document_id || '').toLowerCase();
            vb = String(b.document_id || '').toLowerCase();
          }
          if (va < vb) return -1 * dir;
          if (va > vb) return 1 * dir;
          return 0;
        });
      },

      /** Open the chunk's source document in the Documents view (US4: review the
       *  chunk in its full document context). Switches view + opens the doc detail. */
      viewQuarDocument: function (docID) {
        if (!docID) return;
        this.closeQuarChunk();
        this.switchView('documents');
        this.openDocument(docID);
      },

      // === Vaults (spec 051) ==============================================
      // The shell vault picker + the Vaults view share one /api/vaults fetch.
      // Switching the active vault is a client-side state change (switchVault
      // already sets this.vault, the X-Go-Rag-Vault header); the server confirms
      // a switch only implicitly via subsequent reads.

      /** GET /api/vaults → populate the picker (this.vaults, names) + the view
       *  list (this.vaultsList). Keeps this.vault valid: if the active vault is
       *  no longer listed (deleted from another session), fall back to the
       *  server's active or 'default'. */
      loadVaults: async function () {
        this.vaultsError = '';
        this.vaultsLoading = true;
        try {
          var res = await this.api('/api/vaults');
          if (!res || res.status === 401) return;
          if (!res.ok) { this.vaultsError = 'Failed to load vaults (HTTP ' + res.status + ').'; return; }
          var data = await res.json();
          this.vaultsList = (data && data.vaults) || [];
          this.vaults = this.vaultsList.map(function (v) { return v.name; });
          // Prune the bulk-delete selection to vaults still listed.
          var vpresent = {};
          this.vaultsList.forEach(function (v) { vpresent[v.name] = true; });
          var vkept = {};
          Object.keys(this.vaultChecked || {}).forEach(function (nm) {
            if (vpresent[nm]) { vkept[nm] = true; }
          });
          this.vaultChecked = vkept;
          // Keep the active vault valid; fall back if it vanished.
          if (this.vaults.indexOf(this.vault) < 0) {
            this.vault = (data && data.active) || 'default';
            if (this.vaults.indexOf(this.vault) < 0 && this.vaults.length > 0) {
              this.vault = this.vaults[0];
            }
          }
        } catch (_e) {
          this.vaultsError = 'Network error loading vaults.';
        } finally {
          this.vaultsLoading = false;
        }
      },

      /** Open the create-vault dialog (name input). */
      openVaultCreate: function () {
        this.error = '';
        this.vaultNameDialog = {
          open: true, mode: 'create', title: 'Create vault',
          label: 'Vault name (lowercase letters, digits, hyphens; 1–64)',
          value: '', busy: false, action: 'vault-create', targetVault: '',
        };
      },

      /** Open the rename-vault dialog (new-name input, prefilled). */
      openVaultRename: function (v) {
        this.error = '';
        this.vaultNameDialog = {
          open: true, mode: 'rename', title: 'Rename vault',
          label: 'New name',
          value: v.name, busy: false, action: 'vault-rename', targetVault: v.name,
        };
      },

      closeVaultNameDialog: function () {
        if (this.vaultNameDialog.busy) return; // disable-on-submit
        this.vaultNameDialog.open = false;
      },

      /** Submit the create/rename dialog. Validates client-side, then POSTs. */
      submitVaultNameDialog: async function () {
        var d = this.vaultNameDialog;
        var name = String(d.value || '').trim();
        if (!name) { this.error = 'Name is required.'; return; }
        this.error = '';
        d.busy = true;
        try {
          if (d.action === 'vault-create') {
            var res = await this.api('/api/vaults', { method: 'POST', body: JSON.stringify({ name: name }) });
            if (!res || res.status === 401) return;
            if (res.status === 400) { var e = await res.json().catch(function () { return {}; }); this.error = e.error || 'Invalid or existing name.'; return; }
            if (!res.ok) { this.error = 'Create failed (HTTP ' + res.status + ').'; return; }
          } else if (d.action === 'vault-rename') {
            var r = await this.api('/api/vaults/' + encodeURIComponent(d.targetVault) + '/rename', {
              method: 'POST', body: JSON.stringify({ new_name: name }),
            });
            if (!r || r.status === 401) return;
            if (r.status === 400) { var re = await r.json().catch(function () { return {}; }); this.error = re.error || 'Invalid or existing name.'; return; }
            if (r.status === 404) { this.error = 'Vault not found.'; return; }
            if (!r.ok) { this.error = 'Rename failed (HTTP ' + r.status + ').'; return; }
            // If the active vault was the one renamed, follow it to the new name.
            if (this.vault === d.targetVault) { this.switchVault(name); }
          }
          this.vaultNameDialog.open = false;
          await this.loadVaults();
        } catch (_e) {
          this.error = 'Network error.';
        } finally {
          d.busy = false;
        }
      },

      /** Confirm clearing a vault's contents (keeps it registered). */
      confirmVaultClear: function (v) {
        this.confirmDialog = {
          open: true, title: 'Clear vault',
          message: 'Empty "' + v.name + '"? Every document, chunk, and embedding is removed. The vault stays registered and writable — re-add documents to refill it.',
          confirmLabel: 'Clear', danger: true, busy: false,
          action: 'vault-clear', targetId: v.name, targetLabel: v.name,
        };
      },

      /** Confirm deleting a vault entirely (the default is guarded out). */
      confirmVaultDelete: function (v) {
        if (v.name === 'default') return;
        this.confirmDialog = {
          open: true, title: 'Delete vault',
          message: 'Delete "' + v.name + '" entirely? Its contents are removed AND the vault is unregistered (gone from the list). This cannot be undone.',
          confirmLabel: 'Delete', danger: true, busy: false,
          action: 'vault-delete', targetId: v.name, targetLabel: v.name,
        };
      },

      /** Toggle the page-local vault sort column/direction (mirrors setQuarSort). */
      setVaultSort: function (key) {
        if (this.vaultSortKey === key) {
          this.vaultSortDir = this.vaultSortDir === 'asc' ? 'desc' : 'asc';
        } else {
          this.vaultSortKey = key;
          this.vaultSortDir = 'asc';
        }
      },

      /** Page-local sort of the vault list (active vault always sorts first
       *  regardless of column, so it stays pinned to the top). */
      sortedVaults: function () {
        var key = this.vaultSortKey;
        var dir = this.vaultSortDir === 'desc' ? -1 : 1;
        return (this.vaultsList || []).slice().sort(function (a, b) {
          if (a.active !== b.active) return a.active ? -1 : 1; // active first
          var va, vb;
          if (key === 'documents') {
            va = Number(a.documents) || 0;
            vb = Number(b.documents) || 0;
          } else {
            va = String(a.name || '').toLowerCase();
            vb = String(b.name || '').toLowerCase();
          }
          if (va < vb) return -1 * dir;
          if (va > vb) return 1 * dir;
          return 0;
        });
      },

      /** Number of vaults selected for bulk delete. */
      vaultCheckedCount: function () {
        return Object.keys(this.vaultChecked || {}).length;
      },

      /** True when every DELETABLE (non-default) vault is selected. */
      vaultAllChecked: function () {
        var list = (this.vaultsList || []).filter(function (v) { return v.name !== 'default'; });
        if (list.length === 0) return false;
        var sel = this.vaultChecked || {};
        for (var i = 0; i < list.length; i++) {
          if (!sel[list[i].name]) return false;
        }
        return true;
      },

      /** Toggle one vault's selection (default is not selectable). */
      toggleVaultCheck: function (name) {
        if (name === 'default') return;
        var next = Object.assign({}, this.vaultChecked || {});
        if (next[name]) { delete next[name]; } else { next[name] = true; }
        this.vaultChecked = next;
      },

      /** Select/deselect every deletable (non-default) vault. */
      toggleVaultCheckAll: function () {
        if (this.vaultAllChecked()) {
          this.vaultChecked = {};
          return;
        }
        var next = {};
        (this.vaultsList || []).forEach(function (v) { if (v.name !== 'default') next[v.name] = true; });
        this.vaultChecked = next;
      },

      /** True when the confirm action is allowed. Always true unless a type-to-
       *  confirm phrase is set and the typed input doesn't match it (case-
       *  insensitive). Drives the confirm button's disabled state for the most
       *  destructive actions (vault bulk-delete). */
      confirmArmed: function () {
        var p = this.confirmDialog.phrase;
        if (!p) return true;
        return String(this.confirmDialog.confirmInput || '').toUpperCase() === String(p).toUpperCase();
      },

      /** Confirm deleting every selected vault at once (default is excluded).
       *  Bulk-delete is irreversible — requires typing DELETE to arm the button. */
      confirmVaultBulkDelete: function () {
        var n = this.vaultCheckedCount();
        if (n === 0) return;
        this.confirmDialog = {
          open: true,
          title: 'Delete ' + n + ' vault' + (n === 1 ? '' : 's') + ' — IRREVERSIBLE',
          message: 'Permanently delete ' + n + ' selected vault' + (n === 1 ? '' : 's') + ' AND every document, chunk, and embedding inside them. The vaults are unregistered and their contents are gone. This CANNOT be undone. The default vault is never deletable.',
          confirmLabel: 'Delete ' + n, danger: true, busy: false,
          action: 'vault-bulk-delete', targetId: '', targetLabel: '',
          phrase: 'DELETE', confirmInput: '',
        };
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
