/**
 * runvm — WebBrain content-script driver (shim).
 *
 * WebBrain's content scripts (accessibility-tree.js, content.js) are written
 * for the Chrome/Firefox extension environment: they register their page
 * tools as chrome.runtime.onMessage listeners and answer with sendResponse.
 * runvm's H5 backend is a plain WebKit view with no extension runtime, so
 * this shim (1) stubs the two chrome.runtime entry points the scripts touch,
 * (2) captures the listeners they register, and (3) exposes
 * window.__webbrainRun(action, params) which invokes a captured handler
 * directly and resolves with its sendResponse payload.
 *
 * MIT License — runvm's own code. The two scripts it loads alongside are
 * GPL-3.0-or-later from https://github.com/webbrain-one/webbrain (see
 * README.md in this directory for provenance).
 */
(function () {
  if (window.__webbrainReady) return;

  // The content scripts key their "newest installed copy" guard on this
  // generation marker; give every loaded copy the same value so handlers
  // answer instead of returning early.
  window.__webbrain_generation = 1;

  var listeners = [];
  // The page (a bare WebKit view) has no chrome namespace of its own; if
  // something else defined one, preserve its runtime bits and only override
  // the two entry points the scripts use.
  var existing = window.chrome && window.chrome.runtime;
  window.chrome = window.chrome || {};
  window.chrome.runtime = existing || {};
  window.chrome.runtime.onMessage = window.chrome.runtime.onMessage || {};
  window.chrome.runtime.onMessage.addListener = function (fn) {
    listeners.push(fn);
    return undefined;
  };
  window.chrome.runtime.onMessage.removeListener = function (fn) {
    var i = listeners.indexOf(fn);
    if (i >= 0) listeners.splice(i, 1);
  };
  if (typeof window.chrome.runtime.sendMessage !== 'function') {
    window.chrome.runtime.sendMessage = function (_msg, cb) {
      try { if (cb) cb({}); } catch (e) { /* stub */ }
      return undefined;
    };
  }
  window.chrome.runtime.lastError = null;

  /**
   * Invoke a WebBrain content-script action (get_accessibility_tree, click_ax,
   * type, press_keys, scroll, set_checked, ...) and resolve with the payload
   * the handler would have sendResponse'd to the extension.
   *
   * @param {string} action content-script action name
   * @param {object} params handler parameters (ref_id/text/...)
   * @param {number} timeoutMs hard cap (default 30s)
   * @returns {Promise<object>} the handler's response object
   */
  window.__webbrainRun = function (action, params, timeoutMs) {
    return new Promise(function (resolve) {
      var cap = timeoutMs || 30000;
      var deadline = Date.now() + cap;
      var sender = { url: location.href, tab: { id: 1 }, id: 'runvm-shim' };
      var msg = { target: 'content', action: action, params: params || {}, actionDeadlineAt: deadline };
      var responded = false;
      var done = function (resp) {
        if (responded) return;
        responded = true;
        resolve(resp);
      };
      var settled = false;
      var timer = setTimeout(function () {
        settled = true;
        done({ success: false, noDispatch: true, timedOut: true, error: 'webbrain action timed out after ' + cap + 'ms' });
      }, cap);
      var sync = true;
      for (var i = 0; i < listeners.length; i++) {
        var r;
        try {
          r = listeners[i](msg, sender, done);
        } catch (e) {
          clearTimeout(timer);
          done({ success: false, error: String((e && e.message) || e) });
          return;
        }
        if (r === true) { sync = false; break; } // async: sendResponse will fire
      }
      if (sync && !settled) {
        clearTimeout(timer);
        done({ success: false, error: 'no content handler answered action ' + action });
      }
    });
  };

  window.__webbrainReady = true;
})();
