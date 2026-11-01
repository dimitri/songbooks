// Standalone practice metronome — no song attached. Reuses the Clock and
// Metronome engines from clock.js (same lookahead scheduler, same click
// synthesis), just driven by this page's own controls instead of a song's
// tempo/time/structure.
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

  const bpmInput = document.getElementById('bpm-input');
  const playBtn = document.getElementById('play-pause-btn');
  const tapBtn = document.getElementById('tap-btn');
  const timeSigPicker = document.getElementById('time-sig-picker');
  const accentPreset = document.getElementById('accent-preset');
  const beatPicker = document.getElementById('beat-picker');
  const soundPicker = document.getElementById('sound-picker');
  const beatDotsEl = document.getElementById('beat-dots');

  // beatsPerBar for the dotted-quarter compound meters (6/8, 9/8, 12/8) is
  // the number of main pulses (2, 3, 4), not the numerator — same
  // convention the song viewer uses.
  function beatsForSig(sig) {
    const table = { '2/4': 2, '3/4': 3, '4/4': 4, '5/4': 5, '6/8': 2, '9/8': 3, '12/8': 4 };
    return table[sig] || 4;
  }

  let bpm = 100;
  let beatsPerBar = 4;
  const clock = new Clock(bpm, beatsPerBar);
  const metronome = new Metronome(clock);
  metronome.customAccents = new Set([0]); // downbeat preset by default

  function setBpm(v) {
    bpm = Math.max(30, Math.min(300, v));
    bpmInput.value = String(Math.round(bpm));
    clock.setBpm(bpm);
  }

  bpmInput.addEventListener('change', () => setBpm(parseInt(bpmInput.value, 10) || bpm));
  document.getElementById('bpm-down').addEventListener('click', () => setBpm(bpm - 1));
  document.getElementById('bpm-up').addEventListener('click', () => setBpm(bpm + 1));

  tapBtn.addEventListener('click', () => {
    const t = clock.tapDetermine();
    if (t) setBpm(t);
  });

  function wireIconRow(id, onChange) {
    const row = document.getElementById(id);
    row.addEventListener('click', e => {
      const btn = e.target.closest('button');
      if (!btn) return;
      row.querySelectorAll('button').forEach(b => b.classList.remove('active'));
      btn.classList.add('active');
      row.dataset.value = btn.dataset.value;
      onChange(btn.dataset.value);
    });
  }

  // ---- Beat picker: one toggle per beat in the bar, kept in sync with
  // both the time-signature choice and whichever accent preset was last
  // chosen, and itself editable beat-by-beat for a custom pattern. ------
  function renderBeatPicker() {
    beatPicker.innerHTML = '';
    for (let i = 0; i < beatsPerBar; i++) {
      const b = document.createElement('button');
      b.type = 'button';
      b.textContent = String(i + 1);
      b.dataset.beat = String(i);
      if (metronome.customAccents.has(i)) b.classList.add('active');
      b.addEventListener('click', () => {
        if (metronome.customAccents.has(i)) metronome.customAccents.delete(i);
        else metronome.customAccents.add(i);
        b.classList.toggle('active');
      });
      beatPicker.appendChild(b);
    }
  }

  function applyPreset(name) {
    const set = new Set();
    if (name === 'downbeat') set.add(0);
    else if (name === 'backbeat') { if (beatsPerBar >= 4) { set.add(1); set.add(3); } else if (beatsPerBar === 3) set.add(1); }
    else if (name === 'every') { for (let i = 0; i < beatsPerBar; i++) set.add(i); }
    // 'none' leaves the set empty.
    metronome.customAccents = set;
    renderBeatPicker();
  }

  function isCompoundSig(sig) { return sig === '6/8' || sig === '9/8' || sig === '12/8'; }

  wireIconRow('time-sig-picker', sig => {
    beatsPerBar = beatsForSig(sig);
    clock.beatsPerBar = beatsPerBar;
    // Same reasoning as the song player: the subdivision shown by the
    // beat dots should actually be audible too, softer/duller than the
    // main pulse — not silent.
    metronome.isCompound = isCompoundSig(sig);
    applyPreset(accentPreset.dataset.value);
    renderBeatDots();
  });
  wireIconRow('accent-preset', applyPreset);
  wireIconRow('sound-picker', v => { metronome.sound = v; });

  // ---- Beat dots: same lit-on-the-beat visual as the song player's
  // transport, so this feels like the same instrument, not a different
  // tool. --------------------------------------------------------------
  function renderBeatDots() {
    beatDotsEl.innerHTML = '';
    for (let i = 0; i < beatsPerBar; i++) {
      const d = document.createElement('span');
      d.className = 'beat-dot' + (metronome.customAccents.has(i) ? ' downbeat' : '');
      beatDotsEl.appendChild(d);
    }
  }
  function tickBeatDots() {
    const dots = beatDotsEl.children;
    if (dots.length === beatsPerBar) {
      const idx = clock.running ? Math.floor(clock.currentBeat()) % beatsPerBar : -1;
      for (let i = 0; i < dots.length; i++) dots[i].classList.toggle('lit', i === idx);
    }
    requestAnimationFrame(tickBeatDots);
  }

  const iconPlay = playBtn.innerHTML;
  const iconPause = '<svg class="icon" viewBox="0 0 20 20" fill="currentColor"><rect x="5" y="4" width="3.4" height="12" rx="1"/><rect x="11.6" y="4" width="3.4" height="12" rx="1"/></svg>';
  function setPlaying(playing) {
    if (playing) {
      metronome._ensureCtx();
      clock.start();
      metronome.enabled = true;
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

  renderBeatPicker();
  renderBeatDots();
  tickBeatDots();
  setBpm(bpm);
})();
