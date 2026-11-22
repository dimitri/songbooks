// Setlist click-track mode: a self-contained, deliberately small metronome
// (Clock/Metronome engines from clock.js, same lookahead scheduler as the
// standalone practice tool, but its own compact control surface — a
// performance/rehearsal aid that counts the band in and shows the song's
// click-track note, not the standalone tool's full practice-technique/
// accent/sound-pattern surface) stepping through a pre-loaded setlist.
(() => {
  const themeBtn = document.getElementById('theme-toggle');
  function applyTheme(t) { document.body.dataset.theme = t || ''; }
  try { applyTheme(localStorage.getItem('songbooks-theme') || ''); } catch (e) { /* ignore */ }
  themeBtn.addEventListener('click', () => {
    const order = ['', 'light', 'dark'];
    const next = order[(order.indexOf(document.body.dataset.theme || '') + 1) % order.length];
    applyTheme(next);
    try { localStorage.setItem('songbooks-theme', next); } catch (e) { /* ignore */ }
  });

  const dataEl = document.getElementById('clicktrack-data');
  const songs = JSON.parse(dataEl.textContent || '[]');
  if (!songs.length) return;

  const root = document.getElementById('clicktrack-root');
  const setlistId = root.dataset.setlistId;
  const canEdit = root.dataset.canEdit === '1';

  // Flatten songs into steps, inserting a "break" pseudo-step right
  // before the first song of every set after the first — an explicit
  // step the user advances through, not a silent jump.
  const steps = [];
  songs.forEach(song => {
    if (song.isFirstInSet && song.setIndex > 0) {
      steps.push({ kind: 'break', setLabel: song.setLabel, setIndex: song.setIndex });
    }
    song.step = steps.length;
    steps.push({ kind: 'song', song });
  });

  const posEl = document.getElementById('clicktrack-position');
  const titleEl = document.getElementById('clicktrack-title');
  const metaEl = document.getElementById('clicktrack-meta');
  const entryNoteEl = document.getElementById('clicktrack-entry-note');
  const prevBtn = document.getElementById('clicktrack-prev');
  const nextBtn = document.getElementById('clicktrack-next');
  const nowPlayingCard = document.getElementById('clicktrack-now-playing');
  const breakCard = document.getElementById('clicktrack-break');
  const breakLabel = document.getElementById('clicktrack-break-label');
  const breakContinue = document.getElementById('clicktrack-break-continue');
  const metronomeWrap = document.getElementById('clicktrack-metronome-wrap');
  const songListEl = document.getElementById('clicktrack-songlist');
  const songsFold = document.getElementById('clicktrack-songs-fold');
  const wide = window.matchMedia('(min-width: 960px)');
  // The step shown (a song, or a break between sets).
  let cur = 0;

  // ---- Compact metronome engine — same clock.js classes the standalone
  // page uses, own small control surface. ------------------------------
  const compactEl = document.getElementById('metronome-compact');
  const bpmValueEl = document.getElementById('mc-bpm-value');
  const dotsEl = document.getElementById('mc-beat-dots');
  const playBtn = document.getElementById('mc-play');
  const tapBtn = document.getElementById('mc-tap');
  const muteBtn = document.getElementById('mc-mute');

  // Each song's meter comes worked out from the server (clickMeter in
  // clicktrack_meter.go): the clicks a bar (every eighth in 6/8, 9/8,
  // 12/8 and in odd meters like 7/8), its groups of eighths (5/8 = 3+2,
  // 7/8 = 2+2+3, 12/8 = 3+3+3+3), the clicks with the lighter accent (4
  // in 6/8, 7 in 12/8, 4 and 7 in 9/8, each group's start in 7/8), and
  // how many clicks make one beat of the bpm: 3 in 6/8, 9/8, 12/8, whose
  // bpm is in dotted quarters (so Tap taps the dotted quarter), 1
  // elsewhere.
  const METER_44 = { beats: 4, perBpm: 1, unit: 'quarter', known: true };
  let meter = METER_44;

  let bpm = 120;
  const clock = new Clock(bpm, 4);
  const metronome = new Metronome(clock);
  metronome.customAccents = new Set([0]);

  function setBpm(v) {
    bpm = Math.max(20, Math.min(300, v));
    bpmValueEl.textContent = String(Math.round(bpm));
    clock.setBpm(bpm * (meter.perBpm || 1));
    showOwnTempo();
  }

  // A tempo tapped or nudged (−/+, [ ]) is for this performance only:
  // nothing is saved to the song, and changing song loads that song's
  // own tempo. While it differs, the page says so and offers the way
  // back (as does the bpm in the bar, and the \ key).
  const ownTempoEl = document.getElementById('ct-tempo-own');
  const ownTempoText = document.getElementById('ct-tempo-own-text');
  const ownTempoBack = document.getElementById('ct-tempo-back');
  const bpmResetBtn = document.getElementById('mc-bpm-reset');
  function songTempo() { const st = steps[cur]; return st && st.kind === 'song' ? st.song.tempo : null; }
  function backToSongTempo() { const v = songTempo(); if (v) setBpm(v); }
  function showOwnTempo() {
    const songBpm = songTempo();
    const own = songBpm != null && Math.round(bpm) !== songBpm;
    ownTempoEl.hidden = !own;
    bpmResetBtn.disabled = !own;
    bpmResetBtn.classList.toggle('is-own', own);
    bpmResetBtn.title = own ? t('back to the song’s tempo: %d bpm (\\)', songBpm) : '';
    if (!own) return;
    ownTempoText.textContent = t('%d bpm for this performance only; the song stays at %d bpm.', Math.round(bpm), songBpm);
    ownTempoBack.textContent = t('Back to %d bpm', songBpm);
  }
  ownTempoBack.addEventListener('click', backToSongTempo);
  bpmResetBtn.addEventListener('click', backToSongTempo);
  function setMeter(m) {
    meter = m && m.beats > 0 ? m : METER_44;
    clock.beatsPerBar = meter.beats;
    clock.setBpm(bpm * (meter.perBpm || 1));
    // Accent on 1 (customAccents, above); the lighter ones as the meter says.
    metronome.midAccents = meter.accents && meter.accents.length ? new Set(meter.accents) : null;
    // Every eighth is its own click: no subdivision clicks.
    metronome.isCompound = false;
    applyClickMode();
    renderDots();
  }

  document.getElementById('mc-bpm-down').addEventListener('click', () => setBpm(bpm - 1));
  document.getElementById('mc-bpm-up').addEventListener('click', () => setBpm(bpm + 1));
  // Tap: big, labelled, in the bar. Each tap flashes it; from the second
  // tap it shows the tapped tempo, which the click takes at once. A touch
  // counts on pointerdown (no wait for the finger to lift); the keyboard
  // (T, or Enter on the focused button) through click.
  const tapBpmEl = document.getElementById('ct-tap-bpm');
  let tapPointerAt = -Infinity;
  let tapIdleTimer;
  function tap() {
    const v = clock.tapDetermine();
    tapBtn.classList.remove('is-hit');
    void tapBtn.offsetWidth; // restart the flash
    tapBtn.classList.add('is-hit', 'is-tapping');
    if (v) {
      setBpm(v);
      tapBpmEl.textContent = t('%d bpm', Math.round(bpm));
    } else {
      tapBpmEl.textContent = t('again…');
    }
    clearTimeout(tapIdleTimer);
    tapIdleTimer = setTimeout(() => {
      tapBtn.classList.remove('is-tapping');
      tapBpmEl.textContent = t('tempo');
    }, 4000);
  }
  tapBtn.addEventListener('pointerdown', e => {
    if (e.button !== 0) return;
    tapPointerAt = performance.now();
    tap();
  });
  tapBtn.addEventListener('click', () => {
    if (performance.now() - tapPointerAt < 1500) return; // already counted on pointerdown
    tap();
  });

  // ---- Display/sound preferences: size, portrait/landscape, every-beat/
  // beat-1-only, mute. These persist across every song in the setlist
  // AND silently follow the person's account, not just this browser: a
  // local copy applies instantly (and covers the offline/logged-out
  // case), a server copy (GET on load, debounced PUT on every change) is
  // what makes it "recall whatever I last set, on any device." No
  // explicit save action — every change writes through on its own.
  // (The old 1/2/4-bar option box is gone: the dots always show two
  // bars, as big as the screen allows; a saved "group" is dropped.)
  const PREF_DEFAULTS = { size: 'md', orientation: 'portrait', clickMode: 'every', muted: false };
  let prefs = { ...PREF_DEFAULTS };
  try { Object.assign(prefs, JSON.parse(localStorage.getItem('songbooks-clicktrack-prefs') || '{}')); } catch (e) { /* ignore */ }
  delete prefs.group;

  let savePrefsTimer;
  function savePrefs() {
    try { localStorage.setItem('songbooks-clicktrack-prefs', JSON.stringify(prefs)); } catch (e) { /* ignore */ }
    clearTimeout(savePrefsTimer);
    savePrefsTimer = setTimeout(() => {
      fetch('/api/me/click-track-prefs', {
        method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(prefs),
      }).catch(() => { /* best-effort — the local copy already applied */ });
    }, 500);
  }

  function applyMute() {
    metronome.enabled = !prefs.muted;
    muteBtn.innerHTML = document.getElementById(prefs.muted ? 'icon-speaker-mute-tpl' : 'icon-speaker-tpl').innerHTML;
    muteBtn.setAttribute('aria-pressed', prefs.muted ? 'true' : 'false');
    muteBtn.title = prefs.muted ? t('unmute') : t('mute sound');
  }
  muteBtn.addEventListener('click', () => {
    prefs.muted = !prefs.muted;
    applyMute();
    savePrefs();
  });

  // "Beat 1 only" — both the audible click AND the dot flash fire once
  // per bar instead of every beat, for a drummer who just wants a bar
  // reference, not a full click track. Reuses clock.js's existing
  // gap-click practice mode (play 1 beat, silence the rest of the bar,
  // repeat) rather than a second silencing mechanism — the "gap" cycle
  // is just set to exactly one bar.
  function applyClickMode() {
    if (prefs.clickMode === 'beat1') {
      metronome.practiceMode = 'gap';
      metronome.gapOnBeats = 1;
      metronome.gapOffBeats = Math.max(1, clock.beatsPerBar - 1);
    } else {
      metronome.practiceMode = 'none';
    }
  }

  const pickerEffects = {
    size: v => { compactEl.dataset.size = v; },
    orientation: v => { compactEl.classList.toggle('mc-landscape', v === 'landscape'); },
    clickMode: () => { applyClickMode(); },
  };
  const pickerApply = {};
  function wireSeg(id, field) {
    const row = document.getElementById(id);
    function apply(value) {
      row.querySelectorAll('button').forEach(b => b.classList.toggle('active', b.dataset.value === value));
      row.dataset.value = value;
      pickerEffects[field](value);
    }
    row.addEventListener('click', e => {
      const btn = e.target.closest('button');
      if (!btn) return;
      prefs[field] = btn.dataset.value;
      apply(btn.dataset.value);
      savePrefs();
    });
    pickerApply[field] = apply;
    apply(prefs[field]);
  }
  wireSeg('mc-size-picker', 'size');
  wireSeg('mc-orientation-picker', 'orientation');
  wireSeg('mc-click-picker', 'clickMode');

  // The local copy above applies immediately so the page never flashes
  // defaults-then-settings; the server fetch only overrides fields it
  // actually has a saved value for (omitted/zero fields in the response
  // mean "never set on any device," not "explicitly reset to default"),
  // covering the case where the person's last change was on a different
  // device than this one.
  fetch('/api/me/click-track-prefs').then(r => (r.ok ? r.json() : null)).then(server => {
    if (!server) return;
    let changed = false;
    for (const field of ['size', 'orientation', 'clickMode']) {
      if (server[field] && server[field] !== prefs[field]) { prefs[field] = server[field]; pickerApply[field](server[field]); changed = true; }
    }
    if (typeof server.muted === 'boolean' && server.muted !== prefs.muted) { prefs.muted = server.muted; applyMute(); changed = true; }
    if (changed) { try { localStorage.setItem('songbooks-clicktrack-prefs', JSON.stringify(prefs)); } catch (e) { /* ignore */ } }
  }).catch(() => { /* offline/logged-out — local/default prefs already applied */ });

  // Two bars of dots on one row, always (the old 1/2/4-bar option box is
  // gone; two bars was one of its choices, now the only view): odd bars
  // light the left one, even bars the right one, a bar line between
  // them. One dot per click (per eighth in 6/8, 12/8, 7/8…), a space
  // before each group of eighths and a lighter ring on the lighter
  // accents; --beats, --groups and --dot-gap let the CSS size them to
  // fill the width (capped by the Size option), 24 dots (12/8) still
  // fitting a phone.
  const BARS_SHOWN = 2;
  function renderDots() {
    dotsEl.innerHTML = '';
    const n = clock.beatsPerBar;
    const accents = metronome.midAccents || new Set();
    const starts = new Set();
    let at = 0;
    (meter.groups || []).forEach(g => { if (at > 0) starts.add(at); at += g; });
    dotsEl.style.setProperty('--beats', String(n));
    dotsEl.style.setProperty('--groups', String(starts.size + 1));
    dotsEl.style.setProperty('--dot-gap', n <= 4 ? '0.7rem' : n <= 7 ? '0.4rem' : n <= 9 ? '0.22rem' : '0.12rem');
    for (let b = 0; b < BARS_SHOWN; b++) {
      const bar = document.createElement('span');
      bar.className = 'mc-bar';
      if (n > 6) bar.dataset.many = '';
      for (let k = 0; k < n; k++) {
        const d = document.createElement('span');
        d.className = 'beat-dot' + (k === 0 ? ' downbeat' : '') + (starts.has(k) ? ' group-start' : '') + (accents.has(k) ? ' mid-accent' : '');
        bar.appendChild(d);
      }
      dotsEl.appendChild(bar);
    }
  }
  function tickDots() {
    const n = clock.beatsPerBar;
    const bars = dotsEl.children;
    if (bars.length === BARS_SHOWN && bars[0].children.length === n) {
      const idx = clock.running ? Math.floor(clock.currentBeat()) % (n * BARS_SHOWN) : -1;
      for (let b = 0; b < BARS_SHOWN; b++) {
        const dots = bars[b].children;
        for (let k = 0; k < n; k++) {
          const lit = b * n + k === idx && (prefs.clickMode !== 'beat1' || k === 0);
          dots[k].classList.toggle('lit', lit);
        }
      }
    }
    tickCounters();
    requestAnimationFrame(tickDots);
  }

  // ---- Counters: bar.beat and the song's time since its click started
  // (back to 1.1 and 0:00 on another song; Stop holds the song's time,
  // Play goes on from it), and the set list's total time. The total
  // starts with the first Play and keeps running while the click is
  // stopped between songs (the gig goes on), until someone resets it; it
  // lives in sessionStorage, so it survives a reload of the page during
  // the gig (not a new tab).
  const barBeatEl = document.getElementById('ct-barbeat');
  const songTimeEl = document.getElementById('ct-song-time');
  const setTimeEl = document.getElementById('ct-set-time');
  const totalResetBtn = document.getElementById('ct-total-reset');
  const countersEl = document.getElementById('ct-counters');
  const TOTAL_KEY = 'songbooks-clicktrack-total-' + setlistId;
  let totalStart = null; // Date.now() at the first start, or null
  try {
    const saved = JSON.parse(sessionStorage.getItem(TOTAL_KEY) || 'null');
    if (saved && typeof saved.start === 'number' && saved.start <= Date.now()) totalStart = saved.start;
  } catch (e) { /* no storage: the total lasts as long as the page */ }
  function saveTotal() {
    try {
      if (totalStart == null) sessionStorage.removeItem(TOTAL_KEY);
      else sessionStorage.setItem(TOTAL_KEY, JSON.stringify({ start: totalStart }));
    } catch (e) { /* ignore */ }
  }
  let songMs = 0; // the song's time before the current run
  let songRunFrom = null; // performance.now() when the current run started
  function songClockStart() {
    if (songRunFrom == null) songRunFrom = performance.now();
    if (totalStart == null) { totalStart = Date.now(); saveTotal(); }
  }
  function songClockStop() {
    if (songRunFrom != null) { songMs += performance.now() - songRunFrom; songRunFrom = null; }
  }
  function songClockReset() { songMs = 0; songRunFrom = null; }
  function fmtTime(ms) {
    const s = Math.max(0, Math.floor(ms / 1000));
    const h = Math.floor(s / 3600);
    const m = Math.floor(s / 60) % 60;
    const ss = String(s % 60).padStart(2, '0');
    return h ? `${h}:${String(m).padStart(2, '0')}:${ss}` : `${m}:${ss}`;
  }
  function setText(el, v) { if (el.textContent !== v) el.textContent = v; }
  function tickCounters() {
    const n = clock.beatsPerBar;
    const beat = Math.max(0, Math.floor(clock.currentBeat()));
    setText(barBeatEl, `${Math.floor(beat / n) + 1}.${(beat % n) + 1}`);
    setText(songTimeEl, fmtTime(songMs + (songRunFrom != null ? performance.now() - songRunFrom : 0)));
    setText(setTimeEl, fmtTime(totalStart != null ? Date.now() - totalStart : 0));
    countersEl.classList.toggle('is-running', clock.running);
    countersEl.classList.toggle('is-total-running', totalStart != null);
  }
  // Reset: two taps, so a stray one mid-gig can't zero the total. After
  // a reset the total waits for the next Play (or starts again now if
  // the click is running).
  let resetArmed = null;
  function disarmReset() {
    clearTimeout(resetArmed);
    resetArmed = null;
    totalResetBtn.textContent = '\u21BA';
    totalResetBtn.classList.remove('is-armed');
  }
  totalResetBtn.addEventListener('click', () => {
    if (!resetArmed) {
      totalResetBtn.textContent = t('Reset?');
      totalResetBtn.classList.add('is-armed');
      resetArmed = setTimeout(disarmReset, 3000);
      return;
    }
    disarmReset();
    totalStart = clock.running ? Date.now() : null;
    saveTotal();
  });

  // ---- Keep the screen on while the click runs: the Screen Wake Lock
  // API where there is one (Chrome and Edge 84+, Safari 16.4+, Firefox
  // 126+), asked for on Play and again when the page comes back into
  // view (the browser drops it when the page is hidden); released on
  // Stop and when leaving the page. Without it (iOS before 16.4, older
  // browsers), or if the browser refuses it: a muted, looping, invisible
  // inline video started from the same tap keeps the screen awake (the
  // NoSleep.js way).
  const awakeEl = document.getElementById('ct-awake');
  const awakeLine = document.getElementById('ct-awake-line');
  const KEEP_AWAKE_MP4 = 'data:video/mp4;base64,AAAAIGZ0eXBpc29tAAACAGlzb21pc28yYXZjMW1wNDEAAAMrbW9vdgAAAGxtdmhkAAAAAAAAAAAAAAAAAAAD6AAAB9AAAQAAAQAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAABAAAAAAAAAAAAAAAAAABAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAgAAAlV0cmFrAAAAXHRraGQAAAADAAAAAAAAAAAAAAABAAAAAAAAB9AAAAAAAAAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAABAAAAAAAAAAAAAAAAAABAAAAAABAAAAAQAAAAAAAkZWR0cwAAABxlbHN0AAAAAAAAAAEAAAfQAAAAAAABAAAAAAHNbWRpYQAAACBtZGhkAAAAAAAAAAAAAAAAAABAAAAAgABVxAAAAAAALWhkbHIAAAAAAAAAAHZpZGUAAAAAAAAAAAAAAABWaWRlb0hhbmRsZXIAAAABeG1pbmYAAAAUdm1oZAAAAAEAAAAAAAAAAAAAACRkaW5mAAAAHGRyZWYAAAAAAAAAAQAAAAx1cmwgAAAAAQAAAThzdGJsAAAAuHN0c2QAAAAAAAAAAQAAAKhhdmMxAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAAAABAAEABIAAAASAAAAAAAAAABFUxhdmM2Mi4yOC4xMDIgbGlieDI2NAAAAAAAAAAAAAAAGP//AAAALmF2Y0MBQsAe/+EAFmdCwB7ZHsBEAAADAAQAAAMACDxYuSABAAVoy4PEyAAAABBwYXNwAAAAAQAAAAEAAAAUYnRydAAAAAAAAApQAAAAAAAAABhzdHRzAAAAAAAAAAEAAAACAABAAAAAABRzdHNzAAAAAAAAAAEAAAABAAAAHHN0c2MAAAAAAAAAAQAAAAEAAAACAAAAAQAAABxzdHN6AAAAAAAAAAAAAAACAAACiQAAAAsAAAAUc3RjbwAAAAAAAAABAAADWwAAAGJ1ZHRhAAAAWm1ldGEAAAAAAAAAIWhkbHIAAAAAAAAAAG1kaXJhcHBsAAAAAAAAAAAAAAAALWlsc3QAAAAlqXRvbwAAAB1kYXRhAAAAAQAAAABMYXZmNjIuMTIuMTAyAAAACGZyZWUAAAKcbWRhdAAAAnIGBf//btxF6b3m2Ui3lizYINkj7u94MjY0IC0gY29yZSAxNjUgcjMyMjIgYjM1NjA1YSAtIEguMjY0L01QRUctNCBBVkMgY29kZWMgLSBDb3B5bGVmdCAyMDAzLTIwMjUgLSBodHRwOi8vd3d3LnZpZGVvbGFuLm9yZy94MjY0Lmh0bWwgLSBvcHRpb25zOiBjYWJhYz0wIHJlZj0zIGRlYmxvY2s9MTotMzotMyBhbmFseXNlPTB4MToweDExMSBtZT1oZXggc3VibWU9NyBwc3k9MSBwc3lfcmQ9Mi4wMDowLjcwIG1peGVkX3JlZj0xIG1lX3JhbmdlPTE2IGNocm9tYV9tZT0xIHRyZWxsaXM9MSA4eDhkY3Q9MCBjcW09MCBkZWFkem9uZT0yMSwxMSBmYXN0X3Bza2lwPTEgY2hyb21hX3FwX29mZnNldD0tNCB0aHJlYWRzPTEgbG9va2FoZWFkX3RocmVhZHM9MSBzbGljZWRfdGhyZWFkcz0wIG5yPTAgZGVjaW1hdGU9MSBpbnRlcmxhY2VkPTAgYmx1cmF5X2NvbXBhdD0wIGNvbnN0cmFpbmVkX2ludHJhPTAgYmZyYW1lcz0wIHdlaWdodHA9MCBrZXlpbnQ9MjUwIGtleWludF9taW49MSBzY2VuZWN1dD00MCBpbnRyYV9yZWZyZXNoPTAgcmNfbG9va2FoZWFkPTQwIHJjPWNyZiBtYnRyZWU9MSBjcmY9MjMuMCBxY29tcD0wLjYwIHFwbWluPTAgcXBtYXg9NjkgcXBzdGVwPTQgaXBfcmF0aW89MS40MCBhcT0xOjEuMjAAgAAAAA9liIQF85///w9FAAFXn4AAAAAHQZo4C+c6gA==';
  const awake = (() => {
    let want = false;
    let lock = null;
    let video = null;
    let mode = ''; // 'lock', 'video', or '' (nothing holds the screen)
    function show() {
      const on = want && mode !== '';
      awakeEl.textContent = on ? t('Screen stays on.')
        : want ? t('This browser may let the screen lock: keep it awake in its settings.')
          : t('The screen stays on while the click runs.');
      awakeLine.hidden = !on;
      awakeLine.textContent = on ? t('Screen stays on') : '';
    }
    function playVideo() {
      if (!video) {
        video = document.createElement('video');
        video.setAttribute('playsinline', '');
        video.setAttribute('muted', '');
        video.setAttribute('aria-hidden', 'true');
        video.muted = true;
        video.loop = true;
        video.src = KEEP_AWAKE_MP4; // not in the page: never seen
      }
      const ok = () => { if (want) { mode = 'video'; show(); } else video.pause(); };
      const no = () => { mode = ''; show(); };
      try {
        const p = video.play();
        if (p && p.then) p.then(ok, no); else ok();
      } catch (e) { no(); }
    }
    function acquire() {
      if (!want || document.visibilityState !== 'visible' || mode !== '') return;
      if (!('wakeLock' in navigator)) { playVideo(); return; }
      navigator.wakeLock.request('screen').then(l => {
        if (!want) { l.release().catch(() => {}); return; }
        lock = l;
        mode = 'lock';
        l.addEventListener('release', () => {
          if (lock !== l) return;
          lock = null;
          mode = '';
          show();
        });
        show();
      }, () => playVideo());
    }
    function release() {
      want = false;
      if (lock) { const l = lock; lock = null; l.release().catch(() => {}); }
      if (video) video.pause();
      mode = '';
      show();
    }
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible' && want) acquire();
    });
    window.addEventListener('pagehide', release);
    window.addEventListener('pageshow', e => { if (e.persisted && clock.running) { want = true; acquire(); } });
    show();
    return { start() { want = true; acquire(); }, stop: release, mode: () => mode };
  })();

  const iconPlay = playBtn.innerHTML;
  const iconPause = '<svg class="icon" viewBox="0 0 20 20" fill="currentColor"><rect x="5" y="4" width="3.4" height="12" rx="1"/><rect x="11.6" y="4" width="3.4" height="12" rx="1"/></svg>';
  function setPlaying(playing) {
    if (playing) {
      metronome._ensureCtx();
      clock.start();
      metronome.start();
      songClockStart();
      awake.start();
      playBtn.title = t('stop');
      playBtn.innerHTML = iconPause;
    } else {
      clock.pause();
      metronome.stop();
      clock.seekToBeat(0);
      songClockStop();
      awake.stop();
      playBtn.title = t('start');
      playBtn.innerHTML = iconPlay;
    }
    compactEl.classList.toggle('playing', playing);
    document.getElementById('ct-bar')?.classList.toggle('is-playing', playing);
    try { if ('mediaSession' in navigator) navigator.mediaSession.playbackState = playing ? 'playing' : 'paused'; } catch (e) { /* none */ }
  }
  playBtn.addEventListener('click', () => setPlaying(!clock.running));
  applyMute();
  renderDots();
  tickDots();
  setBpm(bpm);

  // ---- The song's click-track note: big, right under the title, and —
  // for whoever may edit the set list — tap to edit in place, saved on
  // Enter or when the field loses focus (Escape cancels). ---------------
  const noteEl = document.getElementById('clicktrack-clicknote');
  const noteInput = document.getElementById('clicktrack-clicknote-input');
  const noteStatus = document.getElementById('clicktrack-clicknote-status');

  function showNote(song) {
    const text = song.clickNote || '';
    if (!canEdit) {
      noteEl.textContent = text;
      noteEl.hidden = !text;
      return;
    }
    // Still editing (moved on without the field losing focus): the blur
    // saves what was typed against the song it was typed for.
    if (!noteInput.hidden) noteInput.blur();
    noteEl.textContent = text || t('Tap to add a note for this song');
    noteEl.classList.toggle('empty', !text);
    noteStatus.textContent = '';
  }

  let editingSong = null;
  let cancelEdit = false;
  if (canEdit) {
    noteEl.addEventListener('click', () => {
      const step = steps[cur];
      if (step.kind !== 'song') return;
      editingSong = step.song;
      cancelEdit = false;
      noteInput.value = editingSong.clickNote || '';
      noteEl.hidden = true;
      noteInput.hidden = false;
      noteInput.focus();
      noteInput.setSelectionRange(noteInput.value.length, noteInput.value.length);
    });
    noteInput.addEventListener('keydown', e => {
      if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) { e.preventDefault(); noteInput.blur(); }
      else if (e.key === 'Escape') { e.preventDefault(); cancelEdit = true; noteInput.blur(); }
    });
    noteInput.addEventListener('blur', () => {
      const song = editingSong;
      editingSong = null;
      noteInput.hidden = true;
      noteEl.hidden = false;
      if (!song) return;
      const value = noteInput.value.trim();
      if (cancelEdit || value === (song.clickNote || '')) {
        cancelEdit = false;
        if (steps[cur].song === song) showNote(song);
        return;
      }
      saveNote(song, value);
    });
  }

  function saveNote(song, value) {
    const previous = song.clickNote || '';
    // Every entry of this song in the set list shares the note.
    const same = songs.filter(s => s.variantId === song.variantId);
    same.forEach(s => { s.clickNote = value; });
    if (steps[cur].song === song) showNote(song);
    noteStatus.textContent = '';
    fetch(`/api/setlists/${setlistId}/clicktrack-notes/${song.variantId}`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ note: value }),
    }).then(r => (r.ok ? r.json() : Promise.reject(r))).then(res => {
      same.forEach(s => { s.clickNote = res.note || ''; });
      if (steps[cur].song === song) { showNote(song); noteStatus.textContent = t('Saved'); }
      renderSongList();
    }).catch(() => {
      same.forEach(s => { s.clickNote = previous; });
      if (steps[cur].song === song) { showNote(song); noteStatus.textContent = t('Could not save the note.'); }
    });
  }

  // ---- The song list: every song in order under its set's name; tap
  // one to jump there and start the click on it. ------------------------
  document.getElementById('clicktrack-songs-count').textContent = String(songs.length);
  function renderSongList() {
    songListEl.innerHTML = '';
    let lastSet = -1;
    songs.forEach(song => {
      if (song.setIndex !== lastSet && song.setLabel) {
        const h = document.createElement('li');
        h.className = 'clicktrack-songlist-set';
        h.textContent = song.setLabel;
        songListEl.appendChild(h);
      }
      lastSet = song.setIndex;
      const li = document.createElement('li');
      const btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'clicktrack-songlist-item';
      btn.dataset.step = String(song.step);
      if (song.step === cur) btn.setAttribute('aria-current', 'true');
      const num = document.createElement('span');
      num.className = 'clicktrack-songlist-num';
      num.textContent = String(song.index + 1);
      const title = document.createElement('span');
      title.className = 'clicktrack-songlist-title';
      title.textContent = song.title;
      const tempo = document.createElement('span');
      tempo.className = 'clicktrack-songlist-tempo';
      tempo.textContent = `${song.tempo} bpm`;
      btn.append(num, title, tempo);
      li.appendChild(btn);
      songListEl.appendChild(li);
    });
  }
  songListEl.addEventListener('click', e => {
    const btn = e.target.closest('.clicktrack-songlist-item');
    if (!btn) return;
    goTo(parseInt(btn.dataset.step, 10), true);
    // On a phone the panel covers the dots: it closes on a pick.
    if (!wide.matches) shell?.open(false);
  });

  // ---- Stepper: advance through the setlist, re-dialing the compact
  // engine to each song's own tempo/time-signature. ---------------------
  const totalSongs = songs.length;

  // Nothing here counts the song's bars or stops on its own: the click
  // runs on the current song until someone stops it or moves (Previous,
  // Next, a pick in the running order). Moving onto a break step stops.
  function render() {
    const step = steps[cur];
    if (clock.running) setPlaying(false);
    songClockReset();
    songListEl.querySelectorAll('.clicktrack-songlist-item').forEach(b => {
      if (parseInt(b.dataset.step, 10) === cur) b.setAttribute('aria-current', 'true');
      else b.removeAttribute('aria-current');
    });

    countersEl.classList.toggle('is-break', step.kind === 'break');
    showOwnTempo();
    if (step.kind === 'break') {
      nowPlayingCard.hidden = true;
      metronomeWrap.hidden = true;
      breakCard.hidden = false;
      breakLabel.textContent = step.setLabel ? t('Before %s — take a break, then continue when ready.', step.setLabel) : t('Before the next set — take a break, then continue when ready.');
      prevBtn.disabled = cur === 0;
      nextBtn.disabled = false;
      renderBar();
      return;
    }

    breakCard.hidden = true;
    nowPlayingCard.hidden = false;
    metronomeWrap.hidden = false;

    const song = step.song;
    posEl.textContent = `${song.setLabel || t('Set')} · ${t('song %d of %d', song.index + 1, totalSongs)}`;
    titleEl.textContent = song.title;
    metaEl.textContent = [song.artist, song.key ? t('key %s', song.key) : '', tempoLabel(song), song.timeSig + meterLabel(song.meter)]
      .filter(Boolean).join(' · ');
    if (song.note) { entryNoteEl.textContent = song.note; entryNoteEl.hidden = false; }
    else { entryNoteEl.hidden = true; }
    showNote(song);

    setBpm(song.tempo);
    setMeter(song.meter);

    prevBtn.disabled = cur === 0;
    nextBtn.disabled = cur === steps.length - 1;
    renderBar();
  }

  // What the bpm counts, when it isn't a quarter note; a meter the click
  // can't read says it clicks in 4.
  function tempoLabel(song) {
    const unit = song.meter && song.meter.unit;
    if (unit === 'dotted-quarter') return t('%d bpm (dotted quarter)', song.tempo);
    if (unit === 'eighth') return t('%d bpm (eighth)', song.tempo);
    if (unit === 'half') return t('%d bpm (half note)', song.tempo);
    return `${song.tempo} bpm`;
  }
  function meterLabel(m) {
    if (!m || !m.known) return ' ' + t('(clicked in 4/4)');
    if (m.unit === 'eighth' && m.groups && m.groups.length > 1) return ` (${m.groups.join('+')})`;
    return '';
  }

  // Moving to another song while the click runs keeps it running, on
  // the new song from its first beat; picking a song from the list (or
  // "Start next set" after a break) starts it. A break step always stops.
  function goTo(idx, start) {
    if (idx < 0 || idx >= steps.length) return;
    const wasRunning = clock.running;
    cur = idx;
    render();
    if ((start || wasRunning) && steps[cur].kind === 'song') setPlaying(true);
  }

  prevBtn.addEventListener('click', () => goTo(cur - 1, false));
  nextBtn.addEventListener('click', () => goTo(cur + 1, false));
  breakContinue.addEventListener('click', () => goTo(cur + 1, true));

  // ---- The player's bar and panel (player-shell.js) ---------------------
  // The bar: the song before, ▶, the song after, where you are with a copy
  // of the beat dots, the tempo. Its "Running order" opens the panel: the
  // songs and the display and sound options (open in it). The same keys
  // as every player, and the lock screen's (Media Session): the track
  // buttons go to the song before or after.
  const barTitle = document.getElementById('ct-bar-title');
  const barPos = document.getElementById('ct-bar-pos');
  const barDots = document.getElementById('ct-bar-dots');
  songsFold.open = true;
  document.getElementById('clicktrack-options-fold').open = true;
  const shell = window.PlayerShell ? window.PlayerShell.panel({
    root: document.getElementById('ct-panel-wrap'),
    card: document.getElementById('ct-panel'),
    openers: [document.getElementById('ct-panel-btn')],
    closers: document.querySelectorAll('[data-ct-close]'),
    focus: () => document.querySelector(wide.matches ? '.ct-close' : '.ct-grab'),
    swipeIgnore: '.clicktrack-songlist, input, textarea',
    onChange: (on) => { if (on) songListEl.querySelector('[aria-current="true"]')?.scrollIntoView({ block: 'nearest' }); },
  }) : null;
  document.getElementById('ct-panel-btn')?.addEventListener('click', () => shell?.open(!shell.isOpen()));
  function renderBar() {
    const step = steps[cur];
    if (step.kind === 'break') {
      barTitle.textContent = t('Break');
      barPos.textContent = step.setLabel || '';
    } else {
      barTitle.textContent = step.song.title;
      barPos.textContent = t('%d of %d', step.song.index + 1, totalSongs) + (steps[cur + 1] && steps[cur + 1].song ? ' · ' + t('next: %s', steps[cur + 1].song.title) : '');
    }
    media?.meta({ title: barTitle.textContent, artist: step.song ? step.song.artist || '' : '' });
  }
  // The dots, copied as they light.
  if (barDots && window.MutationObserver) {
    const copy = () => { barDots.innerHTML = dotsEl.innerHTML; };
    new MutationObserver(copy).observe(dotsEl, { subtree: true, childList: true, attributes: true, attributeFilter: ['class'] });
    copy();
  }
  const media = window.PlayerShell ? window.PlayerShell.media({
    title: '', play: () => setPlaying(true), pause: () => setPlaying(false),
    previous: () => goTo(cur - 1, false), next: () => goTo(cur + 1, false),
  }) : null;
  // ---- Full screen (fullscreen.js): the header's button, ⇧F; on an
  // iPhone, the line about the Home Screen. The screen stays on as before.
  const fullscreen = window.SongbooksFullscreen ? window.SongbooksFullscreen.setUp({
    button: document.getElementById('ct-fullscreen'),
    hint: document.getElementById('ct-fs-hint'),
    hintKey: 'songbooks:ct-fullscreen-hint',
    cover: true,
  }) : null;
  if (window.PlayerShell) window.PlayerShell.keys({
    title: t('The click track’s keys'),
    own: '#ct-bar, #ct-panel',
    active: () => !document.querySelector('.clicktrack-clicknote-input:not([hidden]):focus'),
    keys: {
      ' ': () => setPlaying(!clock.running), k: () => setPlaying(!clock.running),
      ArrowLeft: () => goTo(cur - 1, false), ArrowRight: () => goTo(cur + 1, false),
      'Shift+ArrowLeft': () => goTo(cur - 1, false), 'Shift+ArrowRight': () => goTo(cur + 1, false),
      // Page-turn pedals send PageUp/PageDown (as in stage mode); a pedal
      // held down moves one song, not the whole set.
      PageUp: (e) => { if (!e.repeat) goTo(cur - 1, false); },
      PageDown: (e) => { if (!e.repeat) goTo(cur + 1, false); },
      '[': () => setBpm(bpm - 1), ']': () => setBpm(bpm + 1), '{': () => setBpm(bpm - 5), '}': () => setBpm(bpm + 5),
      '\\': backToSongTempo,
      m: () => muteBtn.click(), t: () => tapBtn.click(),
      f: () => shell?.open(!shell.isOpen()),
      'Shift+f': () => { if (fullscreen && fullscreen.supported) fullscreen.toggle(); else return false; },
    },
    help: [
      [t('Play / stop'), ' ', 'k'],
      [t('Previous / next song'), 'ArrowLeft', 'ArrowRight'],
      [t('Previous / next song (pedal)'), 'PageUp', 'PageDown'],
      [t('Tempo −1 / +1 (⇧ for 5) · \\ back to the song’s'), '[', ']', '\\'],
      [t('Mute · tap the tempo'), 'm', 't'],
      [t('Open / close the running order · close · these shortcuts'), 'f', 'Escape', '?'],
      ...(fullscreen && fullscreen.supported ? [[t('Full screen'), 'Shift+f']] : []),
    ],
  });

  renderSongList();
  render();
})();
