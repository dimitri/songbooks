// mini-player.js — what was playing goes on from one page to the next.
//
// Every link of the app is a full page load, so the sound stops when one
// is followed. The pages' players register their <audio> here
// (SongbooksNowPlaying.register); when the page is left (pagehide, the tab
// hidden, a pause, and every few seconds while it plays) what plays is
// kept in sessionStorage: a URL to play it again, its title, the page it
// came from, where it was, whether it was playing, its speed and volume.
//
// The next page shows a mini player for it, over its content: the title
// (a link back to its page), play/pause, where it is (a scrubber and the
// time) and × (forget it). Unless one of that page's own players has the
// same recording (owns it): that player takes it over from the same place
// (resume), and no mini player shows — nothing plays twice. A player that
// starts while the mini player plays stops it, and the other way round.
//
// A page left while it played (pagehide) plays on at once where the
// browser lets it. Where it doesn't (Safari, iOS: play() is refused
// without a tap) the mini player says Resume: one tap. A state kept only
// by the periodic save (a tab opened from this one, a page the system
// closed) waits for that tap too: its page may still be playing.
//
// The lock screen's controls (Media Session) work the mini player, and
// then a page's player started after it.
//
// Step 2, keeping the sound alive across a soft navigation, can reuse
// this as it is: show(state) mounts the bar on any page, and the bar's
// <audio> is a player like the others.
//
// The plain logic (the state kept, which player is saved, whether the
// bar shows) is tested by internal/webassets/jstest/mini-player.test.mjs.
(function (root) {
  const STORE = 'songbooks:now-playing';
  const MAX_AGE = 12 * 60 * 60 * 1000; // a state older than that is forgotten
  const SAVE_EVERY = 4000; // ms, while playing
  const SKIP = 15; // seconds, the lock screen's back and forward

  const num = (v, d) => (typeof v === 'number' && isFinite(v) ? v : d);
  const clamp = (v, lo, hi) => Math.min(hi, Math.max(lo, v));
  const str = (v, max) => (typeof v === 'string' ? v.slice(0, max) : '');

  // A source the mini player may play: one of the app's own paths, or an
  // https URL (a pCloud link).
  function safeSrc(src) {
    return typeof src === 'string' && ((src[0] === '/' && src[1] !== '/' && src[1] !== '\\') || /^https:\/\/[^\s]+$/i.test(src));
  }
  // A page to link back to: one of the app's own.
  const safePage = (p) => (typeof p === 'string' && p[0] === '/' && p[1] !== '/' && p[1] !== '\\' ? p : '');

  // What names the same recording on every page: its path and query, the
  // cache-busting v= aside (a song's recording URL carries its size).
  function norm(key) {
    if (typeof key !== 'string') return '';
    const q = key.indexOf('?');
    if (q < 0 || key[0] !== '/') return key;
    const params = key.slice(q + 1).split('&').filter((p) => p && !/^v=/.test(p));
    return key.slice(0, q) + (params.length ? '?' + params.join('&') : '');
  }
  const same = (a, b) => !!a && !!b && norm(a) === norm(b);

  // How a pCloud link is asked for again once it has expired.
  function safeResolve(r) {
    if (!r || r.kind !== 'pcloud') return null;
    const host = str(r.host, 200), code = str(r.code, 200), file = String(r.file || '');
    if (!/^[a-z0-9.-]+$/i.test(host) || !code || !/^\d+$/.test(file)) return null;
    return {kind: 'pcloud', host, code, file};
  }

  // The state kept for a media element playing info ({key, src, title,
  // subtitle, page, resolve}; playing and pos, when given, win over the
  // element's: a mini player waiting for a tap still means to play, from
  // where it was). left: saved as the page goes away. Null when there's
  // nothing to keep.
  function snapshot(media, info, now, left) {
    if (!info || !info.key || !safeSrc(info.src)) return null;
    const dur = num(media.duration, 0);
    const pos = num(info.pos, num(media.currentTime, 0));
    return {
      v: 1,
      key: String(info.key),
      src: info.src,
      title: str(info.title, 300),
      subtitle: str(info.subtitle, 300),
      page: safePage(info.page),
      resolve: safeResolve(info.resolve),
      pos: Math.round(Math.max(0, pos) * 100) / 100,
      dur: Math.round(Math.max(0, dur) * 100) / 100,
      playing: typeof info.playing === 'boolean' ? info.playing : !media.paused,
      rate: clamp(num(media.playbackRate, 1), 0.25, 4),
      volume: clamp(num(media.volume, 1), 0, 1),
      at: now,
      left: !!left,
    };
  }

  // Whether a media element has played to its end: nothing to pick up.
  function finished(media) {
    const d = num(media.duration, 0);
    return !!media.ended || (d > 0 && num(media.currentTime, 0) >= d - 0.25);
  }

  // The state as read back, checked; null when there is none, it is
  // damaged, or it is too old.
  function parse(raw, now) {
    if (!raw) return null;
    let s;
    try { s = JSON.parse(raw); } catch (e) { return null; }
    if (!s || typeof s !== 'object' || s.v !== 1) return null;
    if (typeof s.key !== 'string' || !s.key || !safeSrc(s.src)) return null;
    const at = num(s.at, NaN);
    if (!(now - at < MAX_AGE)) return null;
    const dur = Math.max(0, num(s.dur, 0));
    return {
      v: 1, key: s.key, src: s.src,
      title: str(s.title, 300), subtitle: str(s.subtitle, 300),
      page: safePage(s.page), resolve: safeResolve(s.resolve),
      pos: clamp(num(s.pos, 0), 0, dur > 0 ? dur : Infinity), dur,
      playing: s.playing === true, rate: clamp(num(s.rate, 1), 0.25, 4),
      volume: clamp(num(s.volume, 1), 0, 1), at, left: s.left === true,
    };
  }

  // Whether to start playing again at once: it was playing when its page
  // was left. Kept only by the periodic save, its page may still be
  // playing (a copy of this tab): wait for a tap.
  const autoplay = (s) => !!(s && s.playing && s.left);

  // Where to start again: the place kept, not past the end.
  function startPos(s) {
    if (!s) return 0;
    return s.dur > 1 ? clamp(s.pos, 0, s.dur - 1) : Math.max(0, s.pos);
  }

  // The player to save as the page is left, of those registered: the one
  // playing, else the one that played last; only one that has played on
  // this page (or took over a kept state). Null: keep what's kept.
  function pick(players) {
    let best = null;
    for (const p of players) {
      if (!p.played) continue;
      if (!p.media.paused) return p;
      if (!best || p.lastPlay > best.lastPlay) best = p;
    }
    return best;
  }

  // What the page does with a kept state: {show}, or {owner} when one of
  // its players has that recording.
  function decide(state, players) {
    if (!state) return {show: false};
    for (const p of players) {
      let mine = false;
      try { mine = !!(p.owns && p.owns(state.key)); } catch (e) {}
      if (mine) return {show: false, owner: p};
    }
    return {show: true};
  }

  const fmt = (t) => (!isFinite(t) || t < 0 ? '–:––' : `${Math.floor(t / 60)}:${String(Math.floor(t % 60)).padStart(2, '0')}`);

  // Play media from t: the position is set now (a browser takes it as the
  // start before the file is read) and again once it is read, for one
  // that doesn't. 'playing', 'paused' (play false), or 'blocked' when the
  // browser wants a tap first; another failure throws.
  async function startAt(media, t, play) {
    const at = Math.max(0, num(t, 0));
    if (at > 0) {
      try { media.currentTime = at; } catch (e) {}
      if (media.readyState < 1) {
        media.addEventListener('loadedmetadata', () => {
          if (Math.abs(media.currentTime - at) > 0.5 && (!isFinite(media.duration) || at < media.duration)) media.currentTime = at;
        }, {once: true});
      }
    }
    if (!play) return 'paused';
    try {
      await media.play();
      return 'playing';
    } catch (e) {
      if (e && e.name === 'NotAllowedError') return 'blocked';
      throw e;
    }
  }

  const api = {STORE, MAX_AGE, norm, same, safeSrc, snapshot, finished, parse, autoplay, startPos, pick, decide, fmt, startAt};
  if (typeof module !== 'undefined' && module.exports) { module.exports = api; return; }

  // ---- In the page --------------------------------------------------------

  // Inside a frame (the playlist's song beside it): nothing of this.
  let framed = false;
  try { framed = root.top !== root; } catch (e) { framed = true; }
  if (framed) { root.SongbooksNowPlaying = {register() {}, same, startAt}; return; }

  const t = (s, ...a) => (typeof root.t === 'function' ? root.t(s, ...a) : a.reduce((m, v) => m.replace(/%[sd]/, v), s));
  const load = () => { try { return parse(sessionStorage.getItem(STORE), Date.now()); } catch (e) { return null; } };
  const write = (s) => { try { sessionStorage.setItem(STORE, JSON.stringify(s)); } catch (e) {} };
  const forget = () => { try { sessionStorage.removeItem(STORE); } catch (e) {} };

  const players = []; // {media, info, owns, resume, played, lastPlay}
  let mini = null; // the bar, while it shows: {el, audio, entry, state, ...}
  let lastSave = 0;
  let leaving = false;

  function save(left) {
    if (leaving && !left) return;
    const p = pick(players);
    if (!p) return;
    if (finished(p.media)) { forget(); return; }
    let info = null;
    try { info = p.info(); } catch (e) {}
    const s = snapshot(p.media, info, Date.now(), left);
    if (s) write(s);
  }

  // A player starting: the others stop (one sound at a time), the mini
  // player goes, and the lock screen's controls, if the mini player had
  // them, follow the one playing.
  function started(entry) {
    entry.played = true;
    entry.lastPlay = Date.now();
    for (const p of players) if (p !== entry && !p.media.paused) p.media.pause();
    if (mini && entry !== mini.entry) teardown();
    if (sessionHandled) setSession(entry);
    save(false);
  }

  // A player saves as it plays: when it pauses (or ends: nothing more to
  // keep), and every few seconds.
  function watch(entry) {
    const m = entry.media;
    m.addEventListener('play', () => started(entry));
    m.addEventListener('pause', () => { if (entry.played) save(false); });
    m.addEventListener('timeupdate', () => {
      if (m.paused || !entry.played || Date.now() - lastSave < SAVE_EVERY) return;
      lastSave = Date.now();
      save(false);
    });
  }

  function register(o) {
    if (!o || !o.media) return;
    const entry = {media: o.media, info: o.info || (() => null), owns: o.owns, resume: o.resume, played: false, lastPlay: 0};
    players.push(entry);
    watch(entry);
  }

  // The song page's player plays through Web Audio, and says so.
  document.addEventListener('songbooks:reference-playing', () => teardown());

  // ---- The Media Session: the lock screen, headphones, a car ---------------
  // Set by the mini player when it shows; a page's player that starts
  // after it gets the same controls (the page's own were replaced).
  let sessionHandled = false;
  function setSession(entry) {
    if (!('mediaSession' in navigator)) return;
    const ms = navigator.mediaSession;
    const m = entry.media;
    sessionHandled = true;
    let info = null;
    try { info = entry.info(); } catch (e) {}
    if (info && info.title && typeof MediaMetadata === 'function') {
      try { ms.metadata = new MediaMetadata({title: info.title, artist: info.subtitle || '', artwork: [{src: '/apple-touch-icon.png', sizes: '180x180', type: 'image/png'}]}); } catch (e) {}
    }
    const set = (action, fn) => { try { ms.setActionHandler(action, fn); } catch (e) {} };
    set('play', () => { m.play().catch(() => {}); });
    set('pause', () => m.pause());
    set('seekto', (d) => { if (d && isFinite(d.seekTime)) m.currentTime = d.seekTime; });
    set('seekbackward', (d) => { m.currentTime = Math.max(0, m.currentTime - ((d && d.seekOffset) || SKIP)); });
    set('seekforward', (d) => { m.currentTime = Math.min(num(m.duration, Infinity), m.currentTime + ((d && d.seekOffset) || SKIP)); });
    set('stop', mini && entry === mini.entry ? () => close() : null);
  }
  function sessionPosition(m) {
    if (!('mediaSession' in navigator) || !navigator.mediaSession.setPositionState) return;
    const d = m.duration;
    if (!isFinite(d) || d <= 0) return;
    try { navigator.mediaSession.setPositionState({duration: d, position: Math.min(m.currentTime, d), playbackRate: m.playbackRate || 1}); } catch (e) {}
  }

  // ---- The bar --------------------------------------------------------------
  const PLAY = '<svg class="icon" viewBox="0 0 20 20" aria-hidden="true"><path d="M6 3.8v12.4L16.5 10z" fill="currentColor"/></svg>';
  const PAUSE = '<svg class="icon" viewBox="0 0 20 20" aria-hidden="true"><rect x="5" y="4" width="3.4" height="12" rx="1" fill="currentColor"/><rect x="11.6" y="4" width="3.4" height="12" rx="1" fill="currentColor"/></svg>';
  const CLOSE = '<svg class="icon" viewBox="0 0 20 20" aria-hidden="true"><path d="M5 5l10 10M15 5L5 15" stroke="currentColor" stroke-width="2" stroke-linecap="round"/></svg>';

  function build() {
    const el = document.createElement('div');
    el.className = 'mini-player';
    el.id = 'mini-player';
    el.setAttribute('role', 'region');
    el.setAttribute('aria-label', t('Now playing'));
    el.innerHTML = '<button type="button" class="mp-play"><span class="mp-play-icon"></span><span class="mp-resume"></span></button>'
      + '<div class="mp-text"><a class="mp-title"></a><span class="mp-title mp-plain"></span><small class="mp-sub"></small><small class="mp-status" role="status"></small></div>'
      + '<span class="mp-time" aria-hidden="true"></span>'
      + `<button type="button" class="mp-close">${CLOSE}</button>`
      + '<div class="mp-track"><input type="range" class="mp-seek" min="0" max="1000" step="1" value="0"></div>';
    const x = el.querySelector('.mp-close');
    x.setAttribute('aria-label', t('Close the player'));
    x.title = t('Close the player');
    el.querySelector('.mp-seek').setAttribute('aria-label', t('Position in the recording'));
    return el;
  }

  // The mini player, for a kept state.
  function show(state) {
    teardown();
    const el = build();
    const audio = new Audio();
    audio.preload = 'auto';
    const q = (s) => el.querySelector(s);
    const btn = q('.mp-play'), seek = q('.mp-seek'), link = q('a.mp-title'), plain = q('.mp-plain');
    const sub = q('.mp-sub'), stat = q('.mp-status');
    // want: it means to play (a tap away when the browser blocked it);
    // pending: where it goes once the file is read.
    const self = {el, audio, state, blocked: false, retried: false, want: autoplay(state), pending: null, entry: null};
    mini = self;
    if (state.page) {
      link.href = state.page;
      link.textContent = state.title || state.page;
      link.title = t('Go to its page');
      plain.remove();
    } else {
      plain.textContent = state.title;
      link.remove();
    }
    sub.textContent = state.subtitle;
    sub.hidden = !state.subtitle;
    stat.hidden = true;

    const position = () => (audio.readyState >= 1 ? audio.currentTime : self.pending != null ? self.pending : state.pos);
    const length = () => (audio.readyState >= 1 && isFinite(audio.duration) ? audio.duration : state.dur);
    const render = () => {
      const pos = position(), d = length(), on = !audio.paused;
      const waiting = self.blocked && !on;
      el.classList.toggle('is-playing', on);
      el.classList.toggle('is-blocked', waiting);
      q('.mp-play-icon').innerHTML = on ? PAUSE : PLAY;
      const label = on ? t('Pause') : waiting ? t('Resume at %s', fmt(pos)) : t('Play');
      btn.setAttribute('aria-label', label);
      btn.title = label;
      q('.mp-resume').textContent = waiting ? t('Resume') : '';
      const time = d > 0 ? `${fmt(pos)} / ${fmt(d)}` : fmt(pos);
      q('.mp-time').textContent = time;
      const p = d > 0 ? clamp(pos / d, 0, 1) : 0;
      if (document.activeElement !== seek) seek.value = Math.round(p * 1000);
      seek.setAttribute('aria-valuetext', time);
      q('.mp-track').style.setProperty('--p', p);
      if (on) sessionPosition(audio);
      if (self.lift) self.lift();
    };
    const status = (msg) => {
      stat.textContent = msg || '';
      stat.hidden = !msg;
      sub.hidden = !!msg || !state.subtitle;
    };

    async function go(at, play) {
      self.pending = at;
      let r;
      try { r = await startAt(audio, at, play); } catch (e) { r = 'failed'; }
      if (mini !== self) return;
      self.blocked = r === 'blocked';
      self.pending = null;
      render();
    }

    // A pCloud link lasts a few hours, for the address that asked: asked
    // again, once, when the kept one fails — without a Referer (pCloud
    // refuses another site's).
    async function relink() {
      const r = state.resolve;
      const res = await fetch(`https://${r.host}/getpublinkdownload?code=${encodeURIComponent(r.code)}&fileid=${r.file}`, {referrerPolicy: 'no-referrer'});
      const d = await res.json();
      if (d.result !== 0 || !d.hosts || !d.hosts[0]) throw new Error(d.error || 'pCloud');
      return `https://${d.hosts[0]}${d.path}`;
    }
    audio.addEventListener('error', async () => {
      if (mini !== self || !audio.getAttribute('src')) return;
      if (state.resolve && !self.retried) {
        self.retried = true;
        const at = position();
        try {
          state.src = await relink();
          if (mini !== self) return;
          audio.src = state.src;
          go(at, self.want);
          return;
        } catch (e) {}
      }
      status(t('Couldn’t play it: %s', (audio.error && audio.error.message) || t('the file didn’t load')));
    });

    btn.addEventListener('click', () => {
      if (audio.paused) {
        self.want = true;
        status('');
        // Within the tap itself: what iOS asks for.
        audio.play().then(() => { self.blocked = false; render(); }).catch((e) => {
          if (e && e.name !== 'AbortError' && e.name !== 'NotAllowedError') status(t('Couldn’t play it: %s', e.message || ''));
          render();
        });
      } else {
        self.want = false;
        audio.pause();
      }
    });
    seek.addEventListener('input', () => {
      const d = length();
      if (!(d > 0)) return;
      const at = (seek.value / 1000) * d;
      if (audio.readyState >= 1) audio.currentTime = at;
      else { self.pending = at; startAt(audio, at, false); }
      render();
    });
    q('.mp-close').addEventListener('click', close);
    ['play', 'pause', 'timeupdate', 'loadedmetadata', 'durationchange'].forEach((e) => audio.addEventListener(e, () => { if (mini === self) render(); }));
    audio.addEventListener('ended', () => { if (mini === self) { forget(); teardown(); } });

    // The bar's audio is a player like the page's: kept when the page is
    // left, stopped when another one starts.
    self.entry = {
      media: audio,
      info: () => ({
        key: state.key, src: state.src, title: state.title, subtitle: state.subtitle, page: state.page, resolve: state.resolve,
        playing: audio.paused ? self.blocked && self.want : true,
        pos: audio.readyState >= 1 ? undefined : position(),
      }),
      played: true,
      lastPlay: Date.now(),
    };
    players.push(self.entry);
    watch(self.entry);

    audio.defaultPlaybackRate = state.rate;
    audio.playbackRate = state.rate;
    audio.volume = state.volume;
    audio.src = state.src;
    document.body.append(el);
    document.documentElement.classList.add('has-mini-player');
    // Over a bar fixed at the bottom of the page (the song page's dock),
    // not under it.
    const dock = document.querySelector('.audio-dock');
    if (dock) {
      self.lift = () => {
        const r = dock.getBoundingClientRect();
        el.style.bottom = r.height > 0 && getComputedStyle(dock).position === 'fixed' ? `${Math.round(innerHeight - r.top + 8)}px` : '';
      };
      self.lift();
      addEventListener('resize', self.lift);
      if (typeof ResizeObserver === 'function') (self.watchDock = new ResizeObserver(self.lift)).observe(dock);
    }
    setSession(self.entry);
    render();
    go(startPos(state), self.want);
  }

  // The bar goes; what it played stays kept (another player took over,
  // which keeps its own) — close() forgets it.
  function teardown() {
    if (!mini) return;
    const {el, audio, entry, lift, watchDock} = mini;
    mini = null;
    if (lift) removeEventListener('resize', lift);
    if (watchDock) watchDock.disconnect();
    entry.played = false;
    const i = players.indexOf(entry);
    if (i >= 0) players.splice(i, 1);
    audio.pause();
    audio.removeAttribute('src');
    audio.load();
    el.remove();
    document.documentElement.classList.remove('has-mini-player');
  }
  function close() {
    forget();
    teardown();
    if ('mediaSession' in navigator) {
      try { navigator.mediaSession.metadata = null; navigator.mediaSession.playbackState = 'none'; } catch (e) {}
    }
  }

  // ---- On each page ---------------------------------------------------------
  // What's kept: taken over by the page's player that has it, or the mini
  // player for it.
  function boot() {
    const state = load();
    if (!state) { teardown(); return; }
    const d = decide(state, players);
    if (d.owner) {
      teardown();
      let r;
      try { r = d.owner.resume(state, {play: autoplay(state), at: startPos(state)}); } catch (e) { r = false; }
      Promise.resolve(r).then((ok) => { if (ok === false) show(state); });
      return;
    }
    if (mini && mini.state.key === state.key) return;
    show(state);
  }

  // The save as pagehide comes is the last: Chrome still sends
  // visibilitychange after it, and a pause may follow.
  addEventListener('pagehide', () => { leaving = true; save(true); });
  document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'hidden') save(false); });
  // Back to a page the browser kept (back/forward cache): what played
  // since, on the other pages, is what's kept now.
  addEventListener('pageshow', (e) => { leaving = false; if (e.persisted) boot(); });
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', boot);
  else setTimeout(boot);

  root.SongbooksNowPlaying = {
    // register({media, info, owns, resume}): a page's player. info() is
    // what it plays now ({key, src, title, subtitle, page, resolve}, or
    // null; src a URL another page can play: not a blob). owns(key):
    // whether it has that recording (same(a, b) compares two keys).
    // resume(state, {play, at}): take a kept state over, from at, playing
    // if play (startAt does both); false if it can't: the mini player
    // shows instead.
    register, same, startAt, show, close,
  };
})(typeof window !== 'undefined' ? window : globalThis);
