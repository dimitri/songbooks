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
    renderDots();
  }

  document.getElementById('mc-bpm-down').addEventListener('click', () => setBpm(bpm - 1));
  document.getElementById('mc-bpm-up').addEventListener('click', () => setBpm(bpm + 1));
  tapBtn.addEventListener('click', () => {
    const t = clock.tapDetermine();
    if (t) setBpm(t);
  });

  // Sound on/off — mutes the click without stopping the clock/visuals
  // (Metronome.enabled already gates every scheduled click in clock.js).
  let muted = false;
  try { muted = localStorage.getItem('songbooks-clicktrack-muted') === '1'; } catch (e) { /* ignore */ }
  function applyMute() {
    metronome.enabled = !muted;
    muteBtn.innerHTML = document.getElementById(muted ? 'icon-speaker-mute-tpl' : 'icon-speaker-tpl').innerHTML;
    muteBtn.setAttribute('aria-pressed', muted ? 'true' : 'false');
    muteBtn.title = muted ? 'unmute' : 'mute sound';
  }
  muteBtn.addEventListener('click', () => {
    muted = !muted;
    try { localStorage.setItem('songbooks-clicktrack-muted', muted ? '1' : '0'); } catch (e) { /* ignore */ }
    applyMute();
  });

  // ---- Visual options: size, bar-group, portrait/landscape — a few
  // viewer conveniences remembered locally, not sent anywhere. ----------
  function wireSeg(id, onChange, persistKey, fallback) {
    const row = document.getElementById(id);
    let value = fallback;
    try { value = localStorage.getItem(persistKey) || fallback; } catch (e) { /* ignore */ }
    row.querySelectorAll('button').forEach(b => b.classList.toggle('active', b.dataset.value === value));
    row.dataset.value = value;
    row.addEventListener('click', e => {
      const btn = e.target.closest('button');
      if (!btn) return;
      row.querySelectorAll('button').forEach(b => b.classList.remove('active'));
      btn.classList.add('active');
      row.dataset.value = btn.dataset.value;
      try { localStorage.setItem(persistKey, btn.dataset.value); } catch (err) { /* ignore */ }
      onChange(btn.dataset.value);
    });
    onChange(value);
    return value;
  }

  let groupBars = 1;
  wireSeg('mc-size-picker', v => { compactEl.dataset.size = v; }, 'songbooks-clicktrack-size', 'md');
  wireSeg('mc-group-picker', v => { groupBars = parseInt(v, 10) || 1; renderDots(); }, 'songbooks-clicktrack-group', '1');
  wireSeg('mc-orientation-picker', v => { compactEl.classList.toggle('mc-landscape', v === 'landscape'); }, 'songbooks-clicktrack-orientation', 'portrait');

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
      for (let i = 0; i < dots.length; i++) dots[i].classList.toggle('lit', i === idx);
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
