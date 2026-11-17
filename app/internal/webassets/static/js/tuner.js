// The tuner (tuner.html): listens through the microphone and shows the
// note played, how far from it in cents, and which string of the chosen
// tuning it is. Everything happens in the browser: the sound never leaves
// it.
//
// Pitch is found by the McLeod Pitch Method (McLeod & Wyvill, "A smarter
// way to find pitch", 2005): the normalized square difference function of
// a window of samples, its first peak above 90% of the highest one, and a
// parabola through that peak for the fraction of a sample. It holds up on
// a guitar's strong overtones where plain autocorrelation jumps octaves,
// and needs no library.
(function () {
  const root = document.getElementById('tuner');
  if (!root) return;
  const $ = (id) => document.getElementById(id);
  const NAMES = ['C', 'C♯', 'D', 'E♭', 'E', 'F', 'F♯', 'G', 'A♭', 'A', 'B♭', 'B'];
  const name = (midi) => NAMES[((midi % 12) + 12) % 12];
  const octave = (midi) => Math.floor(midi / 12) - 1;

  // The tunings: the song's first (from its Guitar Pro file), then the
  // usual ones, lowest string first, as MIDI notes.
  const BUILT_IN = [
    {group: 'Guitar', name: 'Standard', notes: [40, 45, 50, 55, 59, 64]},
    {group: 'Guitar', name: 'Drop D', notes: [38, 45, 50, 55, 59, 64]},
    {group: 'Guitar', name: 'E♭ standard', notes: [39, 44, 49, 54, 58, 63]},
    {group: 'Guitar', name: 'D standard', notes: [38, 43, 48, 53, 57, 62]},
    {group: 'Guitar', name: 'Drop C', notes: [36, 43, 48, 53, 57, 62]},
    {group: 'Guitar', name: 'DADGAD', notes: [38, 45, 50, 55, 57, 62]},
    {group: 'Guitar', name: 'Open G', notes: [38, 43, 50, 55, 59, 62]},
    {group: 'Guitar', name: 'Open D', notes: [38, 45, 50, 54, 57, 62]},
    {group: 'Guitar', name: 'Open E', notes: [40, 47, 52, 56, 59, 64]},
    {group: 'Bass', name: '4 strings', notes: [28, 33, 38, 43]},
    {group: 'Bass', name: '5 strings', notes: [23, 28, 33, 38, 43]},
    {group: 'Ukulele', name: 'Standard (GCEA)', notes: [67, 60, 64, 69]},
    {group: 'Any instrument', name: 'Chromatic', notes: []},
  ];
  const song = JSON.parse(root.dataset.songTunings || '[]').map((t) => ({group: root.dataset.songTitle || 'This song', name: t.name, notes: t.notes, capo: t.capo || 0}));
  const tunings = [...song, ...BUILT_IN];

  const select = $('tuner-tuning');
  const groups = {};
  tunings.forEach((t, i) => {
    if (!groups[t.group]) { groups[t.group] = document.createElement('optgroup'); groups[t.group].label = t.group; select.append(groups[t.group]); }
    const o = document.createElement('option');
    o.value = i;
    o.textContent = t.notes.length ? `${t.name} · ${t.notes.map(name).join(' ')}` : t.name;
    groups[t.group].append(o);
  });

  const store = (k, v) => { try { if (v === undefined) return localStorage.getItem(k); localStorage.setItem(k, v); } catch (e) {} return null; };
  let a4 = Number(store('songbooks:tuner:a4')) || 440;
  // A song's own tuning comes first; else the last one used.
  let at = song.length ? 0 : Math.min(tunings.length - 1, Number(store('songbooks:tuner:tuning')) || 0);
  select.value = at;

  const freqOf = (midi) => a4 * Math.pow(2, (midi - 69) / 12);
  const midiOf = (f) => 69 + 12 * Math.log2(f / a4);

  // ---- Strings: one button each, tap for the reference note. --------
  const strings = $('tuner-strings');
  function drawStrings() {
    const t = tunings[at];
    strings.replaceChildren();
    strings.hidden = !t.notes.length;
    t.notes.forEach((n, i) => {
      const b = document.createElement('button');
      b.type = 'button';
      b.className = 'tuner-string';
      b.dataset.midi = n;
      b.innerHTML = `<b></b><small></small>`;
      b.querySelector('b').textContent = name(n);
      b.querySelector('small').textContent = `${t.notes.length - i}`;
      b.setAttribute('aria-label', `String ${t.notes.length - i}, ${name(n)}${octave(n)}: play the note`);
      // Stopped: the string's note. Listening: tune that string only (a
      // tap again lets the tuner pick the string). iOS spoils what a page
      // plays while the microphone is open, so no note then.
      b.addEventListener('click', () => {
        if (!stream) { pluck(n, b); return; }
        locked = locked === n ? null : n;
        strings.querySelectorAll('.tuner-string').forEach((s) => s.classList.toggle('locked', Number(s.dataset.midi) === locked));
      });
      strings.append(b);
    });
    $('tuner-capo').hidden = !t.capo;
    $('tuner-capo').textContent = t.capo ? `Capo on fret ${t.capo}: tune the open strings, then put it on.` : '';
  }
  select.addEventListener('change', () => {
    at = Number(select.value);
    locked = null;
    store('songbooks:tuner:tuning', at);
    drawStrings();
    if (analyser) setWindow();
  });
  drawStrings();

  // ---- A4 calibration. ----------------------------------------------
  const a4Out = $('tuner-a4');
  const showA4 = () => { a4Out.textContent = `A = ${a4} Hz`; store('songbooks:tuner:a4', a4); };
  $('tuner-a4-down').addEventListener('click', () => { a4 = Math.max(425, a4 - 1); showA4(); });
  $('tuner-a4-up').addEventListener('click', () => { a4 = Math.min(455, a4 + 1); showA4(); });
  showA4();

  // ---- Audio. --------------------------------------------------------
  let ctx = null, analyser = null, stream = null, buf = null, raf = 0, wake = null, locked = null;
  // The window: 4096 samples hold a few periods of a guitar's low E; a
  // bass's low B (31 Hz, about 1550 samples a period at 48 kHz) needs 8192.
  const setWindow = () => {
    const low = Math.min(...(tunings[at].notes.length ? tunings[at].notes : [40]));
    analyser.fftSize = low < 36 ? 8192 : 4096;
    buf = new Float32Array(analyser.fftSize);
  };
  const audio = () => (ctx = ctx || new (window.AudioContext || window.webkitAudioContext)());

  // A plucked string (Karplus-Strong), for the reference notes.
  let ringing = null;
  function pluck(midi, btn) {
    const c = audio();
    c.resume();
    const f = freqOf(midi), sr = c.sampleRate, len = Math.floor(sr * 2.5);
    const b = c.createBuffer(1, len, sr), d = b.getChannelData(0);
    const period = Math.round(sr / f), ring = new Float32Array(period);
    for (let i = 0; i < period; i++) ring[i] = Math.random() * 2 - 1;
    for (let i = 0, p = 0; i < len; i++, p = (p + 1) % period) {
      const next = (p + 1) % period;
      d[i] = ring[p];
      ring[p] = 0.996 * 0.5 * (ring[p] + ring[next]);
    }
    if (ringing) ringing.stop();
    const src = c.createBufferSource(), gain = c.createGain();
    gain.gain.value = 0.5;
    src.buffer = b;
    src.connect(gain).connect(c.destination);
    src.start();
    ringing = src;
    strings.querySelectorAll('.tuner-string').forEach((s) => s.classList.toggle('ringing', s === btn));
    src.onended = () => { if (ringing === src) { ringing = null; btn.classList.remove('ringing'); } };
  }

  // McLeod: the normalized square difference of the window with itself,
  // shifted by tau, for tau up to the longest period wanted.
  function detect(x, sr) {
    const n = x.length;
    let rms = 0;
    for (let i = 0; i < n; i++) rms += x[i] * x[i];
    rms = Math.sqrt(rms / n);
    if (rms < 0.008) return null; // silence
    const maxTau = Math.min(n >> 1, Math.ceil(sr / 25));
    const minTau = Math.floor(sr / 1400);
    const nsdf = new Float32Array(maxTau);
    for (let tau = minTau; tau < maxTau; tau++) {
      let acf = 0, m = 0;
      for (let i = 0, j = tau; j < n; i++, j++) { acf += x[i] * x[j]; m += x[i] * x[i] + x[j] * x[j]; }
      nsdf[tau] = m ? (2 * acf) / m : 0;
    }
    // The key maxima: the highest point between each positive-going and
    // negative-going zero crossing.
    const peaks = [];
    let pos = false, best = -1;
    for (let tau = minTau + 1; tau < maxTau - 1; tau++) {
      if (nsdf[tau - 1] <= 0 && nsdf[tau] > 0) { pos = true; best = tau; } else if (pos && nsdf[tau] <= 0) { pos = false; if (best > 0) peaks.push(best); best = -1; }
      if (pos && nsdf[tau] > nsdf[best]) best = tau;
    }
    if (!peaks.length) return null;
    const top = Math.max(...peaks.map((p) => nsdf[p]));
    const tau = peaks.find((p) => nsdf[p] >= 0.9 * top);
    if (nsdf[tau] < 0.8) return null; // not a clear note
    // A parabola through the peak for the fraction of a sample.
    const a = nsdf[tau - 1], b = nsdf[tau], c = nsdf[tau + 1];
    const shift = (a - c) / (2 * (a - 2 * b + c)) || 0;
    return {freq: sr / (tau + shift), clarity: b};
  }

  // ---- Display. -------------------------------------------------------
  // The scale: a tick every 5 cents, longer every 10, the middle the longest.
  const scale = $('tuner-scale');
  for (let t = -50; t <= 50; t += 5) {
    const i = document.createElement('i');
    i.className = t === 0 ? 'zero' : t % 10 === 0 ? 'major' : '';
    i.style.setProperty('--t', t);
    scale.append(i);
  }
  const noteEl = $('tuner-note'), octEl = $('tuner-octave'), centsEl = $('tuner-cents'), hzEl = $('tuner-hz');
  const needle = $('tuner-needle'), meter = $('tuner-meter'), stateEl = $('tuner-state');
  let recent = [], shown = null, inTuneSince = 0, lastHeard = 0;

  function frame(now) {
    raf = requestAnimationFrame(frame);
    analyser.getFloatTimeDomainData(buf);
    const r = detect(buf, ctx.sampleRate);
    if (r) {
      lastHeard = now;
      recent.push(midiOf(r.freq));
      if (recent.length > 5) recent.shift();
      const sorted = [...recent].sort((a, b) => a - b);
      const m = sorted[sorted.length >> 1];
      shown = shown === null || Math.abs(m - shown) > 0.5 ? m : shown + (m - shown) * 0.35;
      show(shown, r.freq, now);
    } else if (now - lastHeard > 1200) {
      recent = [];
      shown = null;
      root.classList.remove('hearing', 'in-tune');
      stateEl.textContent = 'Play a string';
    }
  }

  function show(m, freq, now) {
    const t = tunings[at];
    // Which string: the nearest of the tuning's, within a fourth; else the
    // nearest note.
    let target = Math.round(m);
    if (locked !== null) target = locked;
    else if (t.notes.length) {
      const near = t.notes.reduce((best, n) => (Math.abs(n - m) < Math.abs(best - m) ? n : best), t.notes[0]);
      if (Math.abs(near - m) <= 5) target = near;
    }
    const cents = Math.max(-50, Math.min(50, (m - target) * 100));
    const off = Math.abs(m - target) > 0.5; // a locked string, far off
    noteEl.textContent = name(target);
    octEl.textContent = octave(target);
    centsEl.textContent = `${cents > 0 ? '+' : cents < 0 ? '−' : ''}${Math.abs(Math.round(cents))} ¢`;
    hzEl.textContent = `${freq.toFixed(1)} Hz`;
    needle.style.setProperty('--cents', cents);
    root.classList.add('hearing');
    const close = Math.abs(cents) <= 3;
    if (close && !inTuneSince) inTuneSince = now;
    if (!close) inTuneSince = 0;
    const inTune = close && now - inTuneSince > 350;
    if (inTune && !root.classList.contains('in-tune') && navigator.vibrate) navigator.vibrate(30);
    root.classList.toggle('in-tune', inTune);
    root.classList.toggle('flat', cents < -3);
    root.classList.toggle('sharp', cents > 3);
    stateEl.textContent = inTune ? 'In tune' : off ? `Playing ${name(Math.round(m))}: ${m < target ? 'tighten' : 'loosen'} a lot` : cents < 0 ? 'Too low: tighten' : 'Too high: loosen';
    strings.querySelectorAll('.tuner-string').forEach((s) => {
      const on = Number(s.dataset.midi) === target;
      s.classList.toggle('current', on);
      s.classList.toggle('tuned', on && inTune);
    });
    meter.setAttribute('aria-valuenow', Math.round(cents));
    meter.setAttribute('aria-valuetext', `${name(target)}${octave(target)}, ${Math.round(cents)} cents`);
  }

  // ---- Start and stop. ------------------------------------------------
  const startBtn = $('tuner-start');
  async function start() {
    // iOS lets audio start only within the tap itself: resume first.
    const c = audio();
    c.resume();
    if (ringing) { ringing.stop(); ringing = null; }
    try {
      // The raw sound: no echo cancelling, noise suppression or gain
      // riding, which bend a note's pitch and level.
      stream = await navigator.mediaDevices.getUserMedia({audio: {echoCancellation: false, noiseSuppression: false, autoGainControl: false}});
    } catch (e) {
      stateEl.textContent = 'The tuner needs the microphone. Allow it for this site, then try again.';
      return;
    }
    await c.resume();
    analyser = c.createAnalyser();
    setWindow();
    c.createMediaStreamSource(stream).connect(analyser);
    root.classList.add('listening');
    startBtn.textContent = 'Stop';
    startBtn.setAttribute('aria-pressed', 'true');
    stateEl.textContent = 'Play a string';
    try { wake = await navigator.wakeLock?.request('screen'); } catch (e) {}
    raf = requestAnimationFrame(frame);
  }
  function stop() {
    cancelAnimationFrame(raf);
    stream?.getTracks().forEach((t) => t.stop());
    stream = null;
    locked = null;
    strings.querySelectorAll('.tuner-string').forEach((s) => s.classList.remove('locked', 'current', 'tuned'));
    wake?.release?.();
    wake = null;
    root.classList.remove('listening', 'hearing', 'in-tune', 'flat', 'sharp');
    startBtn.textContent = 'Start the tuner';
    startBtn.setAttribute('aria-pressed', 'false');
    stateEl.textContent = 'Tap Start, then play a string';
  }
  startBtn.addEventListener('click', () => (stream ? stop() : start()));
  document.addEventListener('visibilitychange', () => { if (document.hidden && stream) stop(); });
})();
