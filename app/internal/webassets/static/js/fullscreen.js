// Full screen for a page played on stage (the click track): the page's
// whole screen, no browser bars, a phone turned on its side.
//
// - Where the browser has the Fullscreen API (desktop browsers, Android,
//   iPad): a button (opts.button) puts the page full screen, and back;
//   on a touch screen the screen is then held on its side
//   (screen.orientation.lock, Android Chrome: only while full screen).
//   Leaving it some other way (Esc, a swipe, the back gesture) is seen,
//   the button follows and the screen turns freely again.
// - An iPhone has no full screen for a page: there, the page added to the
//   Home Screen opens as an app, without Safari's bars (the manifest's
//   display, apple-mobile-web-app-capable). opts.hint, a line saying so,
//   shows on an iPhone until it is dismissed, and never in the app.
// The button stays hidden where neither applies.
//
// SongbooksFullscreen.setUp({button, hint, hintKey}) → {toggle, on}.
(function () {
  const d = document;
  const root = d.documentElement;
  const current = () => d.fullscreenElement || d.webkitFullscreenElement || null;
  const supported = () => !!((root.requestFullscreen || root.webkitRequestFullscreen) && (d.fullscreenEnabled || d.webkitFullscreenEnabled));
  const installed = () => window.matchMedia('(display-mode: standalone), (display-mode: fullscreen)').matches || navigator.standalone === true;
  // An iPhone (or iPod): iOS Safari, where pages have no full screen.
  const iPhone = () => /iPhone|iPod/.test(navigator.userAgent);
  const touch = () => window.matchMedia('(pointer: coarse)').matches;

  function lockSideways() {
    try {
      const o = screen.orientation;
      if (o && o.lock && touch()) o.lock('landscape').catch(() => { /* not allowed here: it turns freely */ });
    } catch (e) { /* no orientation lock */ }
  }
  function unlock() {
    try { if (screen.orientation && screen.orientation.unlock) screen.orientation.unlock(); } catch (e) { /* nothing held */ }
  }

  function enter() {
    try {
      const p = root.requestFullscreen ? root.requestFullscreen({ navigationUI: 'hide' }) : root.webkitRequestFullscreen();
      if (p && p.then) p.then(lockSideways, () => { /* refused (no tap, or not allowed) */ });
      else lockSideways();
    } catch (e) { /* refused */ }
  }
  function exit() {
    unlock();
    try {
      const p = d.exitFullscreen ? d.exitFullscreen() : d.webkitExitFullscreen && d.webkitExitFullscreen();
      if (p && p.catch) p.catch(() => {});
    } catch (e) { /* already out */ }
  }

  function setUp(opts) {
    const btn = opts.button;
    const hint = opts.hint;
    const on = () => !!current();
    const toggle = () => { if (on()) exit(); else enter(); };

    if (btn && supported()) {
      btn.hidden = false;
      const enterTitle = btn.dataset.titleEnter || btn.title;
      const paint = () => {
        const v = on();
        btn.setAttribute('aria-pressed', String(v));
        btn.title = v ? (btn.dataset.titleExit || enterTitle) : enterTitle;
        btn.setAttribute('aria-label', btn.title);
        d.body.classList.toggle('is-fullscreen', v);
      };
      btn.addEventListener('click', toggle);
      // However it is left: the button, Esc, a swipe, the system's back.
      const changed = () => { if (!on()) unlock(); paint(); };
      d.addEventListener('fullscreenchange', changed);
      d.addEventListener('webkitfullscreenchange', changed);
      paint();
    }

    if (hint && !supported() && iPhone() && !installed()) {
      let seen = false;
      try { seen = localStorage.getItem(opts.hintKey) === '1'; } catch (e) { /* show it */ }
      hint.hidden = seen;
      hint.querySelector('button')?.addEventListener('click', () => {
        hint.hidden = true;
        try { localStorage.setItem(opts.hintKey, '1'); } catch (e) { /* this page only */ }
      });
    }
    return { toggle, on, supported: supported() };
  }

  window.SongbooksFullscreen = { setUp, supported, installed };
})();
