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
