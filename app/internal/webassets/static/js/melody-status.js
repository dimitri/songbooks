// melody-status.js — where a song's melody stands until its Melody view
// can draw it (docs/melody-sheet.md "Until it's ready"; the server's
// GET /api/songs/{id}/melody/status, melody_status.go): the voice
// separation queued or running, the melody being found, or why there's
// none. melody-view.js shows it on the view's tab and in the view, and
// asks again while it can change.
//
// MelodyStatus.poller(opts) asks again after the server's poll seconds,
// longer each time nothing moved (×1.5, at most a minute), paused while
// the page is hidden, and stops for good on a state that doesn't change
// by itself (ready, failed, none…).
//
// The plain logic is tested by internal/webassets/jstest/melody-status.test.mjs.
(function (root) {
  // States that don't change by themselves.
  const DONE = new Set(['none', 'unavailable', 'failed', 'unsung', 'sync', 'ready']);
  const MAX_DELAY = 60;

  function done(st) { return !st || DONE.has(st.state); }

  // moved: the state or its progress changed since the last answer.
  function moved(prev, cur) {
    if (!prev || !cur) return true;
    if (prev.state !== cur.state || prev.position !== cur.position) return true;
    return Math.round((prev.progress || 0) * 100) !== Math.round((cur.progress || 0) * 100);
  }

  // The next wait, in seconds: the server's when something moved, else
  // half as long again as the last, at most MAX_DELAY.
  function nextDelay(prev, cur, last) {
    const base = Math.max(2, (cur && cur.poll) || 15);
    if (moved(prev, cur) || !last) return base;
    return Math.min(MAX_DELAY, Math.round(last * 1.5));
  }

  // The tab's short note: "42%" while splitting, "…" while waiting.
  function tabNote(st) {
    if (!st || done(st)) return '';
    if (typeof st.progress === 'number' && st.state === 'running') return `${Math.round(st.progress * 100)}%`;
    return '…';
  }

  // poller({ fetch: () => Promise<status>, onStatus(st), setTimeout,
  // clearTimeout, hidden: () => bool }) — fetch is asked first after
  // start(first)'s own poll (first: the status the page came with).
  // Returns { start(first), stop(), now(), resume(), running() }.
  function poller(opts) {
    const set = opts.setTimeout || ((f, ms) => setTimeout(f, ms));
    const clear = opts.clearTimeout || ((t) => clearTimeout(t));
    const hidden = opts.hidden || (() => false);
    let timer = null, last = null, delay = 0, stopped = false, paused = false, busy = false;

    function schedule(prev, cur) {
      if (timer) { clear(timer); timer = null; }
      if (stopped || done(cur)) { stopped = true; return; }
      delay = nextDelay(prev, cur, delay);
      timer = set(tick, delay * 1000);
    }
    async function tick() {
      timer = null;
      if (stopped) return;
      if (hidden()) { paused = true; return; }
      if (busy) return;
      busy = true;
      let cur = null;
      try {
        cur = await opts.fetch();
      } catch (e) {
        cur = null;
      }
      busy = false;
      if (stopped) return;
      if (!cur) {
        // A failed request: tried again later, as if nothing moved.
        delay = Math.min(MAX_DELAY, Math.max(delay, 5) * 2);
        timer = set(tick, delay * 1000);
        return;
      }
      const prev = last;
      last = cur;
      opts.onStatus(cur, prev);
      schedule(prev, cur);
    }
    return {
      start(first) {
        last = first || null;
        stopped = false;
        if (first && done(first)) { stopped = true; return; }
        if (!first) { tick(); return; }
        schedule(null, first);
      },
      stop() { stopped = true; if (timer) { clear(timer); timer = null; } },
      // Ask now (a retry queued, the page shown again).
      now() { if (timer) { clear(timer); timer = null; } stopped = false; delay = 0; tick(); },
      resume() { if (paused && !stopped) { paused = false; tick(); } },
      running() { return !stopped; },
      last() { return last; },
    };
  }

  const api = { done, moved, nextDelay, tabNote, poller, DONE, MAX_DELAY };
  if (typeof module !== 'undefined' && module.exports) { module.exports = api; return; }
  root.MelodyStatus = api;
})(typeof window !== 'undefined' ? window : globalThis);
