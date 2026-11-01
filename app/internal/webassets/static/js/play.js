(function () {
  const transportEl = document.getElementById('transport');
  const bpm = parseFloat(transportEl.dataset.bpm) || 120;
  const beatsPerBar = parseInt(transportEl.dataset.beatsPerBar, 10) || 4;

  const clock = new Clock(bpm, beatsPerBar);
  const metronome = new Metronome(clock);

  // Flatten the rendered sheet once: every .bar in document order already
  // carries its global bar index (assigned server-side), and every section
  // knows its first bar (if it has a grid at all — a section with no grid,
  // e.g. one that implicitly reuses an earlier section's chords, has no
  // bars to sync to and its jump button is disabled below).
  const bars = [...document.querySelectorAll('.bar')];
  const sections = [...document.querySelectorAll('.sheet-section')].map(el => {
    const firstBar = el.querySelector('.bar');
    return { el, index: parseInt(el.dataset.sectionIndex, 10), startBar: firstBar ? parseInt(firstBar.dataset.globalBar, 10) : null };
  });

  document.querySelectorAll('.section-jump').forEach(btn => {
    const sec = sections[parseInt(btn.dataset.sectionIndex, 10)];
    if (!sec || sec.startBar == null) {
      btn.disabled = true;
      btn.title = 'no chord grid in this section yet — jump unavailable';
    }
  });

  let currentBarEl = null;
  let currentSectionEl = null;

  function highlightTick() {
    const bar = clock.currentBar();
    const barEl = bars.find(b => parseInt(b.dataset.globalBar, 10) === bar) || null;
    if (barEl !== currentBarEl) {
      currentBarEl?.classList.remove('current');
      barEl?.classList.add('current');
      currentBarEl = barEl;
    }
    const sectionEl = barEl ? barEl.closest('.sheet-section') : null;
    if (sectionEl !== currentSectionEl) {
      currentSectionEl?.classList.remove('current-section');
      sectionEl?.classList.add('current-section');
      if (sectionEl) sectionEl.scrollIntoView({ block: 'start', behavior: 'smooth' });
      currentSectionEl = sectionEl;
    }
    requestAnimationFrame(highlightTick);
  }
  requestAnimationFrame(highlightTick);

  const playPauseBtn = document.getElementById('play-pause-btn');
  const bpmDisplay = document.getElementById('bpm-display');

  function updateBpmDisplay() { bpmDisplay.textContent = Math.round(clock.bpm) + ' bpm'; }
  updateBpmDisplay();

  function setPlaying(playing) {
    if (playing) {
      clock.start();
      metronome.start();
      playPauseBtn.innerHTML = '&#10074;&#10074;';
    } else {
      clock.pause();
      metronome.stop();
      playPauseBtn.innerHTML = '&#9654;';
    }
  }

  playPauseBtn.addEventListener('click', () => setPlaying(!clock.running));

  document.getElementById('tostart-btn').addEventListener('click', () => {
    clock.seekToBar(0);
  });
  document.getElementById('rewind4-btn').addEventListener('click', () => {
    clock.seekToBar(Math.max(0, clock.currentBar() - 4));
  });
  document.getElementById('rewind8-btn').addEventListener('click', () => {
    clock.seekToBar(Math.max(0, clock.currentBar() - 8));
  });

  document.getElementById('tap-btn').addEventListener('click', () => {
    if (!clock.running) {
      const bpmGuess = clock.tapDetermine();
      if (bpmGuess) { clock.setBpm(bpmGuess); updateBpmDisplay(); }
    } else {
      clock.tapResync();
      updateBpmDisplay();
    }
  });

  document.getElementById('sound-select').addEventListener('change', e => { metronome.sound = e.target.value; });
  document.getElementById('metro-enabled').addEventListener('change', e => { metronome.enabled = e.target.checked; });

  document.querySelectorAll('.section-jump').forEach(btn => {
    btn.addEventListener('click', async () => {
      const sec = sections[parseInt(btn.dataset.sectionIndex, 10)];
      if (!sec || sec.startBar == null) return;
      const wasPlaying = clock.running;
      btn.disabled = true;
      metronome._ensureCtx();
      await metronome.countIn(4, clock.bpm);
      clock.seekToBar(sec.startBar);
      btn.disabled = false;
      if (wasPlaying || !clock.running) setPlaying(true);
    });
  });

  // View mode (chords+lyrics / jazz chart / lyrics-only) and fit-to-page are
  // pure CSS/layout switches over the same server-rendered DOM — no
  // re-render needed, so switching modes can never drift from the data.
  const sheet = document.getElementById('sheet');
  const fitCheckbox = document.getElementById('fit-enabled');

  function applyFit() {
    sheet.style.fontSize = '';
    if (!fitCheckbox.checked) return;
    const floor = 9;
    let size = 22;
    sheet.style.fontSize = size + 'px';
    const limit = () => window.innerHeight - sheet.getBoundingClientRect().top - 12;
    let guard = 0;
    while (size > floor && sheet.scrollHeight > limit() && guard++ < 60) {
      size -= 0.5;
      sheet.style.fontSize = size + 'px';
    }
  }

  document.getElementById('mode-select').addEventListener('change', e => {
    sheet.dataset.renderMode = e.target.value;
    // Jazz mode's whole point is one page, even on a phone — default fit on;
    // the user can still uncheck it to scroll instead.
    if (e.target.value === 'jazz') { fitCheckbox.checked = true; }
    applyFit();
  });
  fitCheckbox.addEventListener('change', applyFit);
  window.addEventListener('resize', () => { if (fitCheckbox.checked) applyFit(); });

  document.getElementById('presentation-toggle')?.addEventListener('click', () => {
    const body = document.body;
    body.dataset.mode = body.dataset.mode === 'presentation' ? 'view' : 'presentation';
  });

  // Manual light/dark override on top of the OS preference, remembered
  // per-viewer only (browser storage can legitimately be empty/throw — a
  // remembered theme is a convenience, never load-bearing).
  const themeBtn = document.getElementById('theme-toggle');
  function applyTheme(t) { document.body.dataset.theme = t || ''; }
  try {
    applyTheme(localStorage.getItem('songbooks-theme') || '');
  } catch (e) { /* ignore */ }
  themeBtn?.addEventListener('click', () => {
    const order = ['', 'light', 'dark'];
    const cur = document.body.dataset.theme || '';
    const next = order[(order.indexOf(cur) + 1) % order.length];
    applyTheme(next);
    try { localStorage.setItem('songbooks-theme', next); } catch (e) { /* ignore */ }
  });
})();
