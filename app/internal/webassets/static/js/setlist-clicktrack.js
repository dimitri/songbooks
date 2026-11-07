// Setlist click-track mode: a self-contained, deliberately small metronome
// (Clock/Metronome engines from clock.js, same lookahead scheduler as the
// standalone practice tool, but its own compact control surface — a
// performance/rehearsal aid that counts the band in and shows the next
// song's note, not the standalone tool's full practice-technique/accent/
// sound-pattern surface) stepping through a pre-loaded setlist.
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

  // Flatten songs into steps, inserting a "break" pseudo-step right
  // before the first song of every set after the first — an explicit
  // step the user advances through, not a silent jump.
  const steps = [];
  songs.forEach(song => {
    if (song.isFirstInSet && song.setIndex > 0) {
      steps.push({ kind: 'break', setLabel: song.setLabel, setIndex: song.setIndex });
    }
    steps.push({ kind: 'song', song });
  });

  const posEl = document.getElementById('clicktrack-position');
  const titleEl = document.getElementById('clicktrack-title');
  const metaEl = document.getElementById('clicktrack-meta');
  const noteEl = document.getElementById('clicktrack-note');
  const prevBtn = document.getElementById('clicktrack-prev');
  const nextBtn = document.getElementById('clicktrack-next');
  const nowPlayingCard = document.getElementById('clicktrack-now-playing');
  const breakCard = document.getElementById('clicktrack-break');
  const breakLabel = document.getElementById('clicktrack-break-label');
  const breakContinue = document.getElementById('clicktrack-break-continue');
  const metronomeWrap = document.getElementById('clicktrack-metronome-wrap');

  // ---- Compact metronome engine — same clock.js classes the standalone
  // page uses, own small control surface. ------------------------------
  const compactEl = document.getElementById('metronome-compact');
  const bpmValueEl = document.getElementById('mc-bpm-value');
  const dotsEl = document.getElementById('mc-beat-dots');
  const playBtn = document.getElementById('mc-play');
  const tapBtn = document.getElementById('mc-tap');
  const muteBtn = document.getElementById('mc-mute');

  function beatsForSig(sig) {
    const table = { '2/4': 2, '3/4': 3, '4/4': 4, '5/4': 5, '6/8': 2, '9/8': 3, '12/8': 4 };
    return table[sig] || 4;
  }
  function isCompoundSig(sig) { return sig === '6/8' || sig === '9/8' || sig === '12/8'; }

  let bpm = 120;
  const clock = new Clock(bpm, 4);
  const metronome = new Metronome(clock);
  metronome.customAccents = new Set([0]);

  function setBpm(v) {
    bpm = Math.max(20, Math.min(300, v));
    bpmValueEl.textContent = String(Math.round(bpm));
    clock.setBpm(bpm);
  }
  function setTimeSig(sig) {
    clock.beatsPerBar = beatsForSig(sig);
    metronome.isCompound = isCompoundSig(sig);
    applyClickMode();
    renderDots();
  }

  document.getElementById('mc-bpm-down').addEventListener('click', () => setBpm(bpm - 1));
  document.getElementById('mc-bpm-up').addEventListener('click', () => setBpm(bpm + 1));
  tapBtn.addEventListener('click', () => {
    const t = clock.tapDetermine();
    if (t) setBpm(t);
  });

  // ---- Display/sound preferences: size, bar-group, portrait/landscape,
  // every-beat/beat-1-only, mute. These persist across every song in the
  // setlist (nothing here resets on prev/next — see render() below) AND
  // silently follow the person's account, not just this browser: a local
  // copy applies instantly (and covers the offline/logged-out case), a
  // server copy (GET on load, debounced PUT on every change) is what
  // actually makes it "recall whatever I last set, on any device." No
  // explicit save action — every change writes through on its own.
  const PREF_DEFAULTS = { size: 'md', group: '1', orientation: 'portrait', clickMode: 'every', muted: false };
  let prefs = { ...PREF_DEFAULTS };
  try { Object.assign(prefs, JSON.parse(localStorage.getItem('songbooks-clicktrack-prefs') || '{}')); } catch (e) { /* ignore */ }

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
    muteBtn.title = prefs.muted ? 'unmute' : 'mute sound';
  }
  muteBtn.addEventListener('click', () => {
    prefs.muted = !prefs.muted;
    applyMute();
    savePrefs();
  });

  let groupBars = parseInt(prefs.group, 10) || 1;
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
    group: v => { groupBars = parseInt(v, 10) || 1; renderDots(); },
    orientation: v => { compactEl.classList.toggle('mc-landscape', v === 'landscape'); },
    clickMode: v => { applyClickMode(); },
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
  wireSeg('mc-group-picker', 'group');
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
    for (const field of ['size', 'group', 'orientation', 'clickMode']) {
      if (server[field] && server[field] !== prefs[field]) { prefs[field] = server[field]; pickerApply[field](server[field]); changed = true; }
    }
    if (typeof server.muted === 'boolean' && server.muted !== prefs.muted) { prefs.muted = server.muted; applyMute(); changed = true; }
    if (changed) { try { localStorage.setItem('songbooks-clicktrack-prefs', JSON.stringify(prefs)); } catch (e) { /* ignore */ } }
  }).catch(() => { /* offline/logged-out — local/default prefs already applied */ });

  function renderDots() {
    dotsEl.innerHTML = '';
    const n = clock.beatsPerBar * groupBars;
    for (let i = 0; i < n; i++) {
      const d = document.createElement('span');
      d.className = 'beat-dot' + (i % clock.beatsPerBar === 0 ? ' downbeat' : '');
      dotsEl.appendChild(d);
    }
  }
  function tickDots() {
    const n = clock.beatsPerBar * groupBars;
    const dots = dotsEl.children;
    if (dots.length === n) {
      const idx = clock.running ? Math.floor(clock.currentBeat()) % n : -1;
      for (let i = 0; i < dots.length; i++) {
        const lit = i === idx && (prefs.clickMode !== 'beat1' || i % clock.beatsPerBar === 0);
        dots[i].classList.toggle('lit', lit);
      }
    }
    requestAnimationFrame(tickDots);
  }

  const iconPlay = playBtn.innerHTML;
  const iconPause = '<svg class="icon" viewBox="0 0 20 20" fill="currentColor"><rect x="5" y="4" width="3.4" height="12" rx="1"/><rect x="11.6" y="4" width="3.4" height="12" rx="1"/></svg>';
  function setPlaying(playing) {
    if (playing) {
      metronome._ensureCtx();
      clock.start();
      metronome.start();
      playBtn.title = 'stop';
      playBtn.innerHTML = iconPause;
    } else {
      clock.pause();
      metronome.stop();
      clock.seekToBeat(0);
      playBtn.title = 'start';
      playBtn.innerHTML = iconPlay;
    }
  }
  playBtn.addEventListener('click', () => setPlaying(!clock.running));
  applyMute();
  renderDots();
  tickDots();
  setBpm(bpm);

  // ---- Stepper: advance through the setlist, re-dialing the compact
  // engine to each song's own tempo/time-signature. ---------------------
  const totalSongs = songs.length;
  let i = 0;

  function render() {
    const step = steps[i];
    if (clock.running) setPlaying(false);

    if (step.kind === 'break') {
      nowPlayingCard.hidden = true;
      metronomeWrap.hidden = true;
      breakCard.hidden = false;
      breakLabel.textContent = `Before ${step.setLabel || 'the next set'} — take a break, then continue when ready.`;
      prevBtn.disabled = i === 0;
      nextBtn.disabled = false;
      return;
    }

    breakCard.hidden = true;
    nowPlayingCard.hidden = false;
    metronomeWrap.hidden = false;

    const song = step.song;
    posEl.textContent = `${song.setLabel || 'Set'} · song ${song.index + 1} of ${totalSongs}`;
    titleEl.textContent = song.title;
    metaEl.textContent = [song.artist, song.key ? `key ${song.key}` : '', `${song.tempo} bpm`, song.timeSig]
      .filter(Boolean).join(' · ');
    if (song.note) { noteEl.textContent = song.note; noteEl.hidden = false; }
    else { noteEl.hidden = true; }

    setBpm(song.tempo);
    setTimeSig(song.timeSig);

    prevBtn.disabled = i === 0;
    nextBtn.disabled = i === steps.length - 1;
  }

  prevBtn.addEventListener('click', () => { if (i > 0) { i--; render(); } });
  nextBtn.addEventListener('click', () => { if (i < steps.length - 1) { i++; render(); } });
  breakContinue.addEventListener('click', () => { if (i < steps.length - 1) { i++; render(); } });

  render();
})();
