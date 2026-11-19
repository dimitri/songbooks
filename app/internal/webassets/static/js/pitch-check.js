// pitch-check.js — "Check my singing" on the song page: the singer sings
// over the recording (in their key, the voice down), and sees their pitch
// against the song's own vocal line, with a score at the end of a take.
//
// The line to sing comes from the recording's voice part (cmd/stemsd's
// "vocals"): decoded here and tracked note by note (YIN, de Cheveigné &
// Kawahara 2002) in a worker, once per recording (kept in IndexedDB). The
// microphone is tracked the same way as it's sung. Compared octave aside
// (a man singing a woman's song an octave down is singing it right), with
// a little time play (±0.15 s) for the latencies; moved by the Key.
// A take's score, for a coaching student playing a song of their
// songbook, joins their practice log (/api/practice/pitch, practice.go).
(function () {
  const root = document.getElementById('audio-player');
  if (!root || !window.AudioContext || !navigator.mediaDevices || !window.indexedDB) return;
  // The player sets its clock up as it starts: wait for it.
  if (!window.songbooksReferenceClock) {
    let tries = 0;
    const wait = setInterval(() => {
      if (window.songbooksReferenceClock) { clearInterval(wait); init(); } else if (++tries > 40) clearInterval(wait);
    }, 250);
  } else init();
  function init() {

  // ---- Pitch tracking (YIN) ----------------------------------------------
  // In the worker too: its source is this function's.
  function yin(buf, sr, minF, maxF, thresh) {
    const maxLag = Math.min(Math.floor(sr / minF), buf.length >> 1);
    const minLag = Math.max(2, Math.floor(sr / maxF));
    const W = buf.length - maxLag;
    let rms = 0;
    for (let i = 0; i < buf.length; i++) rms += buf[i] * buf[i];
    rms = Math.sqrt(rms / buf.length);
    // the difference function, cumulative-mean normalized
    const d = new Float32Array(maxLag + 1);
    d[0] = 1;
    let sum = 0;
    for (let tau = 1; tau <= maxLag; tau++) {
      let s = 0;
      for (let i = 0; i < W; i++) { const x = buf[i] - buf[i + tau]; s += x * x; }
      sum += s;
      d[tau] = sum > 0 ? (s * tau) / sum : 1;
    }
    // the first dip under the threshold, at its bottom; else the deepest
    let best = -1;
    for (let tau = minLag; tau <= maxLag; tau++) {
      if (d[tau] < thresh) {
        while (tau + 1 <= maxLag && d[tau + 1] < d[tau]) tau++;
        best = tau;
        break;
      }
    }
    if (best < 0) {
      let m = 1;
      for (let tau = minLag; tau <= maxLag; tau++) if (d[tau] < m) { m = d[tau]; best = tau; }
      if (best < 0 || m > 0.3) return { f: 0, rms, clarity: 0 };
    }
    let t = best;
    if (best > 1 && best < maxLag) {
      const a = d[best - 1], b = d[best], c = d[best + 1];
      const den = a - 2 * b + c;
      if (den > 0) t = best + (a - c) / (2 * den);
    }
    return { f: sr / t, rms, clarity: 1 - d[best] };
  }
  const midiOf = (f) => 69 + 12 * Math.log2(f / 440);

  // The worker: a recording's voice, mono at 11025 Hz, to a pitch line, a
  // value a hop (MIDI, NaN where no one sings).
  const workerSrc = `
    ${yin.toString()}
    onmessage = (e) => {
      const { samples, sr, hop, win } = e.data;
      let peak = 0;
      for (let i = 0; i < samples.length; i++) peak = Math.max(peak, Math.abs(samples[i]));
      const gate = Math.max(0.01, peak * 0.06);
      const n = Math.max(0, Math.floor((samples.length - win) / hop));
      const out = new Float32Array(n);
      for (let k = 0; k < n; k++) {
        const r = yin(samples.subarray(k * hop, k * hop + win), sr, 70, 1300, 0.15);
        out[k] = r.f > 0 && r.rms > gate && r.clarity > 0.75 ? 69 + 12 * Math.log2(r.f / 440) : NaN;
        if (k % 500 === 0) postMessage({ progress: k / n });
      }
      // A median of five against octave slips; alone, a frame doesn't count.
      const clean = new Float32Array(n).fill(NaN);
      for (let k = 2; k < n - 2; k++) {
        const w = [out[k - 2], out[k - 1], out[k], out[k + 1], out[k + 2]].filter((x) => !isNaN(x));
        if (w.length >= 3 && !isNaN(out[k])) { w.sort((a, b) => a - b); clean[k] = w[w.length >> 1]; }
      }
      postMessage({ line: clean }, [clean.buffer]);
    };`;

  const HOP = 256, WIN = 1024, SR = 11025, STEP = HOP / SR; // ~23 ms a value

  // ---- The reference line, kept per recording -----------------------------
  const idb = () => new Promise((res, rej) => {
    const r = indexedDB.open('songbooks-pitch', 1);
    r.onupgradeneeded = () => r.result.createObjectStore('lines');
    r.onsuccess = () => res(r.result);
    r.onerror = () => rej(r.error);
  });
  async function cached(key) {
    try {
      const db = await idb();
      return await new Promise((res) => { const q = db.transaction('lines').objectStore('lines').get(key); q.onsuccess = () => res(q.result || null); q.onerror = () => res(null); });
    } catch (e) { return null; }
  }
  async function keep(key, line) {
    try { const db = await idb(); db.transaction('lines', 'readwrite').objectStore('lines').put(line, key); } catch (e) { /* private window: computed again next time */ }
  }
  async function referenceLine(clock, onProgress) {
    const key = `${clock.variantId}:${clock.category}:${clock.meta.byteSize || ''}`;
    const have = await cached(key);
    if (have) return have;
    const url = `/song/${clock.variantId}/audio.mp3?category=${clock.category}&stem=vocals&v=${clock.meta.byteSize || ''}`;
    const bytes = await fetch(url).then((r) => { if (!r.ok) throw new Error(r.status); return r.arrayBuffer(); });
    const ac = new AudioContext();
    const decoded = await ac.decodeAudioData(bytes);
    ac.close();
    const off = new OfflineAudioContext(1, Math.ceil(decoded.duration * SR), SR);
    const src = off.createBufferSource();
    src.buffer = decoded;
    src.connect(off.destination);
    src.start();
    const mono = (await off.startRendering()).getChannelData(0);
    const worker = new Worker(URL.createObjectURL(new Blob([workerSrc], { type: 'text/javascript' })));
    const line = await new Promise((res, rej) => {
      worker.onmessage = (e) => { if (e.data.progress !== undefined) onProgress(e.data.progress); else res(e.data.line); };
      worker.onerror = rej;
      worker.postMessage({ samples: mono, sr: SR, hop: HOP, win: WIN }, [mono.buffer]);
    });
    worker.terminate();
    keep(key, line);
    return line;
  }


  // ---- Notes ------------------------------------------------------------------
  // The line cut into notes, as research scores singing (a note in tune
  // within so many cents of its target, Pfordresher & Demorest 2021):
  // steady stretches of 0.12 s or more, each at its median pitch.
  function notesOf(line) {
    const out = [];
    const med = (a) => { const s = a.slice().sort((x, y) => x - y); return s[s.length >> 1]; };
    for (let k = 0; k < line.length; k++) {
      if (isNaN(line[k])) continue;
      const start = k, vals = [line[k]];
      while (k + 1 < line.length && !isNaN(line[k + 1]) && Math.abs(line[k + 1] - med(vals)) < 0.6) vals.push(line[++k]);
      if ((k - start + 1) * STEP >= 0.12) {
        const prev = out[out.length - 1];
        // each value stands for the middle of its window
        const at = start * STEP + WIN / SR / 2;
        // a note after a rest starts clearly: its timing is judged
        out.push({ start: at, end: (k + 1) * STEP + WIN / SR / 2, midi: med(vals), afterRest: !prev || at - prev.end >= 0.1 });
      }
    }
    return out;
  }

  // How strict, by level: a note in tune within so many cents, on time
  // within so many milliseconds. Beginner: the right note, give or take a
  // semitone (the line past which research calls a singer "poor-pitch");
  // intermediate: research's own "accurate" (±50, half a semitone);
  // advanced: about what a listener hears as in tune (20–30 cents).
  const LEVELS = {
    beginner: { cents: 100, ms: 200, label: () => t('Beginner') },
    intermediate: { cents: 50, ms: 120, label: () => t('Intermediate') },
    advanced: { cents: 25, ms: 70, label: () => t('Advanced') },
  };
  const LEVEL_KEY = 'songbooks:pitch-level';
  const storedLevel = () => { try { return localStorage.getItem(LEVEL_KEY); } catch (e) { return null; } };

  // A note against what was sung (frames [time on the recording, midi]),
  // the sung times moved back by lag: its pitch (the median of the note's
  // steady part, octave aside) and its onset (the first frame near its
  // pitch around its start).
  function judge(note, frames, lag, key, lv) {
    const target = note.midi + key;
    const fold = (m) => { let d = m - target; d -= 12 * Math.round(d / 12); return d * 100; };
    const trim = Math.min(0.08, (note.end - note.start) * 0.25);
    let all = 0, devs = [];
    let onset = null, quiet = 0;
    for (const [tm, m] of frames) {
      const rt = tm - lag;
      // the singer comes in: a voice near the note after at least 50 ms of
      // silence, around the note's start (legato through the rest: none)
      if (note.afterRest && onset === null && rt >= note.start - 0.3 && rt <= note.start + 0.4) {
        if (isNaN(m) || Math.abs(fold(m)) > 200) quiet += 1;
        else { if (quiet >= 3) onset = rt - note.start; quiet = 0; }
      }
      if (rt < note.start + trim || rt > note.end) continue;
      all++;
      if (!isNaN(m)) devs.push(fold(m));
    }
    if (!all || devs.length < all * 0.4) return { sung: false };
    devs.sort((a, b) => a - b);
    const cents = devs[devs.length >> 1];
    const a = Math.abs(cents);
    return { sung: true, cents, hit: a <= lv.cents, close: a <= 2 * lv.cents, onset };
  }

  // A take's notes judged, with the lag that fits best: the device's own
  // latency, unless a clearly better one (Bluetooth headphones) is found;
  // a plateau keeps the device's, so a singer coming in late stays late.
  function score(notes, frames, key, lv, lag0) {
    const span = frames.length ? [frames[0][0] - lag0, frames[frames.length - 1][0] - lag0] : [0, 0];
    const inTake = notes.filter((n) => n.start >= span[0] && n.end <= span[1] + 0.2);
    const hitsAt = (lag) => inTake.reduce((s, n) => s + (judge(n, frames, lag, key, lv).hit ? 1 : 0), 0);
    // Pitch barely depends on a few hundred ms (notes last); only a big
    // device delay (Bluetooth headphones) moves the lag.
    let best = hitsAt(lag0), lag = lag0;
    for (let l = lag0 - 0.4; l <= lag0 + 0.41; l += 0.05) { const h = hitsAt(l); if (h > best * 1.3 + 2) { best = h; lag = l; } }
    const judged = inTake.map((n) => ({ note: n, ...judge(n, frames, lag, key, lv) }));
    const sung = judged.filter((j) => j.sung);
    const hits = sung.filter((j) => j.hit).length;
    const timed = sung.filter((j) => j.onset !== null);
    const near = sung.filter((j) => j.close).map((j) => j.cents).sort((a, b) => a - b);
    const onsets = timed.map((j) => j.onset * 1000).sort((a, b) => a - b);
    // Placement: how steadily each entry falls against the others, the
    // take's own median offset taken out (the device's latency is in it,
    // and no browser says it exactly).
    const offset = onsets.length ? onsets[onsets.length >> 1] : 0;
    timed.forEach((j) => { j.onTime = Math.abs(j.onset * 1000 - offset) <= lv.ms; });
    const onTime = timed.filter((j) => j.onTime).length;
    const spread = onsets.length ? Math.round(onsets.map((o) => Math.abs(o - offset)).sort((a, b) => a - b)[onsets.length >> 1]) : 0;
    return {
      judged, notes: inTake.length, sung: sung.length, hits, timed: timed.length, onTime,
      pitch: sung.length ? Math.round((100 * hits) / sung.length) : 0,
      rhythm: timed.length ? Math.round((100 * onTime) / timed.length) : 0,
      cents: near.length ? Math.round(near[near.length >> 1]) : 0,
      ms: spread,
      lag,
    };
  }

  // ---- The panel ------------------------------------------------------------
  const panel = document.createElement('section');
  panel.className = 'pitch-check';
  panel.hidden = true;
  panel.innerHTML = `
    <div class="pitch-check-head">
      <button type="button" class="pitch-check-toggle"></button>
      <span class="pitch-check-levels" role="group"></span>
      <span class="hint pitch-check-status" aria-live="polite"></span>
    </div>
    <div class="pitch-check-goal" hidden></div>
    <canvas class="pitch-check-lane" height="150" hidden></canvas>
    <div class="pitch-check-result" hidden></div>
    <div class="pitch-check-history" hidden></div>`;
  root.after(panel);
  const $p = (sel) => panel.querySelector(sel);
  const toggle = $p('.pitch-check-toggle'), status = $p('.pitch-check-status'), lane = $p('.pitch-check-lane');
  const result = $p('.pitch-check-result'), goal = $p('.pitch-check-goal'), history = $p('.pitch-check-history'), levels = $p('.pitch-check-levels');
  toggle.textContent = t('Check my singing');

  // What the coach set for this song (a target, a level) and the takes so
  // far: a coaching student's song of their songbook, else nothing.
  const ctx = window.songbooksPracticeContext;
  let info = null;
  let level = LEVELS[storedLevel()] ? storedLevel() : 'beginner';
  function renderLevels() {
    levels.innerHTML = '';
    if (info && info.level) {
      const s = document.createElement('span');
      s.className = 'pitch-level-fixed';
      s.textContent = LEVELS[info.level].label();
      s.title = t('the level your coach set for this song');
      levels.append(s);
      return;
    }
    for (const [k, lv] of Object.entries(LEVELS)) {
      const b = document.createElement('button');
      b.type = 'button';
      b.textContent = lv.label();
      b.title = t('in tune within %d cents, on time within %d ms', lv.cents, lv.ms);
      b.setAttribute('aria-pressed', String(k === level));
      b.addEventListener('click', () => { level = k; try { localStorage.setItem(LEVEL_KEY, k); } catch (e) { /* kept for this page */ } renderLevels(); });
      levels.append(b);
    }
  }
  const lv = () => LEVELS[(info && info.level) || level];

  function renderGoal() {
    if (!info || (!info.pitchTarget && !info.rhythmTarget)) { goal.hidden = true; return; }
    const bar = (label, best, target) => {
      if (!target) return '';
      const pct = Math.min(100, Math.round((100 * (best || 0)) / target));
      return `<div class="goal-row"><span>${label}</span><span class="goal-bar"><i style="width:${pct}%"></i><b style="left:100%"></b></span><span>${best || 0}% / ${target}%</span></div>`;
    };
    goal.innerHTML = `<p class="goal-title">${t('Your coach’s goal for this song')}${info.reached ? ` <span class="goal-reached">${t('reached!')}</span>` : ''}</p>`
      + bar(t('In tune'), info.best && info.best.pitch, info.pitchTarget)
      + bar(t('On time'), info.best && info.best.rhythm, info.rhythmTarget);
    goal.hidden = false;
  }
  function renderHistory() {
    if (!info || !info.takes || !info.takes.length) { history.hidden = true; return; }
    // the last takes, oldest to newest: two little bar charts
    const takes = info.takes.slice(-12);
    const bars = (key, cls) => takes.map((tk) => `<i class="${cls}" style="height:${Math.max(4, tk[key])}%" title="${tk[key]}%"></i>`).join('');
    history.innerHTML = `<div class="history-chart"><span>${t('In tune')}</span><span class="history-bars">${bars('pitch', 'is-pitch')}</span></div>
      <div class="history-chart"><span>${t('On time')}</span><span class="history-bars">${bars('rhythm', 'is-rhythm')}</span></div>
      <p class="hint">${tn(takes.length, 'Your last take', 'Your last %d takes')}${info.streak > 1 ? ' · ' + t('%d days of practice in a row', info.streak) : ''}</p>`;
    history.hidden = false;
  }
  async function loadInfo() {
    if (!ctx) return;
    try {
      const r = await fetch(`/api/practice/pitch?songbookId=${ctx.songbookId}&variantId=${ctx.variantId}`);
      if (r.status === 200) info = await r.json();
    } catch (e) { /* no info: still checks, keeps nothing */ }
    renderLevels(); renderGoal(); renderHistory();
  }
  renderLevels();

  // Shown when the playing recording has its voice apart.
  let infoAsked = false;
  const refresh = () => {
    const c = window.songbooksReferenceClock();
    panel.hidden = !(c && c.category && c.stems.includes('vocals'));
    if (!panel.hidden && !infoAsked) { infoAsked = true; loadInfo(); }
  };
  refresh();
  setInterval(refresh, 2000);

  let on = false, line = null, notes = [], lineKey = '', mic = null, ac = null, analyser = null, raf = 0;
  let frames = []; // the take: [time on the recording, midi]
  let wasPlaying = false, lag0 = 0.15;

  async function start() {
    const clock = window.songbooksReferenceClock();
    on = true;
    toggle.textContent = t('Stop checking');
    toggle.classList.add('is-on');
    result.hidden = true;
    const key = `${clock.variantId}:${clock.category}`;
    if (!line || lineKey !== key) {
      status.textContent = t('Listening to the song’s voice…');
      try {
        line = await referenceLine(clock, (p) => { status.textContent = t('Listening to the song’s voice… %d%', Math.round(p * 100)); });
        notes = notesOf(line);
        lineKey = key;
      } catch (e) {
        status.textContent = t('Couldn’t read the song’s voice.');
        stop();
        return;
      }
    }
    try {
      mic = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: false, noiseSuppression: false, autoGainControl: false } });
    } catch (e) {
      status.textContent = t('The microphone isn’t allowed: allow it for this site, then try again.');
      stop();
      return;
    }
    ac = new AudioContext();
    analyser = ac.createAnalyser();
    analyser.fftSize = 4096;
    ac.createMediaStreamSource(mic).connect(analyser);
    lag0 = (ac.outputLatency || 0) + (ac.baseLatency || 0) + 0.05;
    lane.hidden = false;
    status.textContent = t('Sing along: headphones on, the voice down. Play the song.');
    frames = [];
    loop();
  }

  function stop() {
    on = false;
    toggle.textContent = t('Check my singing');
    toggle.classList.remove('is-on');
    cancelAnimationFrame(raf);
    if (mic) mic.getTracks().forEach((tr) => tr.stop());
    if (ac) ac.close();
    mic = ac = analyser = null;
    lane.hidden = true;
    endTake();
  }
  toggle.addEventListener('click', () => (on ? stop() : start()));

  // The microphone's pitch now: 48 kHz averaged down by 4, YIN.
  const raw = new Float32Array(4096), small = new Float32Array(1024);
  function micPitch() {
    analyser.getFloatTimeDomainData(raw);
    const f = ac.sampleRate / 4;
    for (let i = 0; i < 1024; i++) small[i] = (raw[4 * i] + raw[4 * i + 1] + raw[4 * i + 2] + raw[4 * i + 3]) / 4;
    const r = yin(small, f, 70, 1300, 0.15);
    return r.f > 0 && r.rms > 0.01 && r.clarity > 0.8 ? midiOf(r.f) : NaN;
  }

  function loop() {
    raf = requestAnimationFrame(loop);
    const clock = window.songbooksReferenceClock();
    const key = (window.songbooksPlayerKey && window.songbooksPlayerKey.get()) || 0;
    const midi = micPitch();
    if (clock.playing) {
      if (frames.length && clock.time < frames[frames.length - 1][0] - 0.5) endTake(); // jumped back: a new take
      // the pitch heard over the analyser's last window: its middle
      frames.push([clock.time - analyser.fftSize / ac.sampleRate / 2, midi]);
    } else if (wasPlaying) {
      endTake();
    }
    wasPlaying = clock.playing;
    draw(clock.time - lag0, key);
  }

  // ---- Drawing: the notes to sing as bars, coloured once sung; the
  // singer's pitch as dots --------------------------------------------------
  const live = new Map(); // note -> verdict, for the notes passed in this take
  function draw(now, key) {
    const w = lane.clientWidth, h = 150, dpr = window.devicePixelRatio || 1;
    if (lane.width !== Math.round(w * dpr)) { lane.width = Math.round(w * dpr); lane.height = Math.round(h * dpr); }
    const g = lane.getContext('2d');
    g.setTransform(dpr, 0, 0, dpr, 0, 0);
    g.clearRect(0, 0, w, h);
    const from = now - 4, to = now + 2;
    const x = (tm) => ((tm - from) / (to - from)) * w;
    const inView = notes.filter((n) => n.end > from && n.start < to);
    const mids = inView.map((n) => n.midi + key).sort((a, b) => a - b);
    const mid = mids.length ? mids[mids.length >> 1] : 60;
    const y = (m) => h / 2 - (m - mid) * (h / 20);
    const css = getComputedStyle(panel);
    const col = { todo: css.getPropertyValue('--pitch-ref').trim() || '#bbb', hit: '#2e9e5b', close: '#d9962b', off: '#c8483b', missed: 'rgba(128,128,128,0.35)' };
    const recent = frames.slice(-600);
    for (const n of inView) {
      if (n.end < now - 0.3 && !live.has(n)) live.set(n, judge(n, recent, lag0, key, lv()));
      const v = live.get(n);
      g.fillStyle = !v ? col.todo : !v.sung ? col.missed : v.hit ? col.hit : v.close ? col.close : col.off;
      const x0 = x(n.start), x1 = Math.max(x0 + 3, x(n.end) - 1);
      g.beginPath(); g.roundRect ? g.roundRect(x0, y(n.midi + key) - 4, x1 - x0, 8, 4) : g.rect(x0, y(n.midi + key) - 4, x1 - x0, 8); g.fill();
    }
    g.fillStyle = 'rgba(128,128,128,0.3)';
    g.fillRect(x(now) - 1, 0, 2, h);
    for (const [tm, m] of recent) {
      const rt = tm - lag0;
      if (isNaN(m) || rt < from) continue;
      let mm = m; mm -= 12 * Math.round((mm - mid) / 12);
      const n = notes.find((nn) => rt >= nn.start - 0.05 && rt <= nn.end + 0.05);
      let c = col.todo;
      if (n) { let d = (mm - (n.midi + key)) * 100; d -= 1200 * Math.round(d / 1200); const a = Math.abs(d); c = a <= lv().cents ? col.hit : a <= 2 * lv().cents ? col.close : col.off; }
      g.fillStyle = c;
      g.beginPath(); g.arc(x(rt), y(mm), 2.5, 0, 7); g.fill();
    }
  }

  // ---- A take's end: stars, the scores, the goal, kept --------------------
  const clockTime = (s) => `${Math.floor(s / 60)}:${String(Math.floor(s % 60)).padStart(2, '0')}`;
  const stars = (p, target) => { const goalAt = target || 70; return p >= goalAt ? 3 : p >= goalAt * 0.75 ? 2 : p >= goalAt * 0.4 ? 1 : 0; };
  async function endTake() {
    const fr = frames;
    frames = [];
    live.clear();
    if (fr.length < 240 || !notes.length) return; // under ~4 s of singing: no score
    const key = (window.songbooksPlayerKey && window.songbooksPlayerKey.get()) || 0;
    const s = score(notes, fr, key, lv(), lag0);
    if (s.sung < 4) return;
    const target = info && info.pitchTarget;
    const n = stars(s.pitch, target);
    const tendency = s.pitch < 40 ? t('the notes are often far: listen to the line again, with the voice up')
      : Math.abs(s.cents) < 10 ? t('right in the middle of the notes')
      : s.cents < 0 ? t('a little flat (%d cents): aim a touch higher', -s.cents) : t('a little sharp (%d cents): aim a touch lower', s.cents);
    const timing = s.timed < 3 ? t('not enough entries to tell') : s.ms <= 60 ? t('steady, your entries fall together') : s.ms <= 150 ? t('a little uneven (%d ms): count the rests', s.ms) : t('uneven (%d ms): clap the rhythm, then sing it', s.ms);
    result.innerHTML = `
      <div class="take-stars" aria-label="${t('%d stars out of 3', n)}">${[0, 1, 2].map((i) => `<span class="${i < n ? 'is-on' : ''}">★</span>`).join('')}</div>
      <div class="take-scores">
        <div><strong>${s.pitch}%</strong><span>${t('in tune')}</span><small>${t('%d of %d notes', s.hits, s.sung)}</small></div>
        <div><strong>${s.rhythm}%</strong><span>${t('on time')}</span><small>${t('%d of %d notes', s.onTime, s.timed)}</small></div>
      </div>
      <p>${t('Pitch: %s.', tendency)} ${t('Timing: %s.', timing)}</p>`;
    const slips = s.judged.filter((j) => j.sung && !j.hit).map((j) => Math.floor(j.note.start / 5) * 5);
    const counts = {};
    slips.forEach((x0) => { counts[x0] = (counts[x0] || 0) + 1; });
    const worst = Object.entries(counts).filter(([, c]) => c >= 2).sort((a, b) => b[1] - a[1]).slice(0, 3).map(([x0]) => Number(x0)).sort((a, b) => a - b);
    if (worst.length) {
      const q = document.createElement('p');
      q.append(t('Where it slips: '));
      worst.forEach((s0, i) => {
        const b = document.createElement('button');
        b.type = 'button'; b.className = 'link-btn';
        b.textContent = `${clockTime(s0)}–${clockTime(s0 + 5)}`;
        b.addEventListener('click', () => window.songbooksReferenceSeek(Math.max(0, s0 - 2)));
        q.append(i ? ', ' : '', b);
      });
      result.append(q);
    }
    result.hidden = false;
    if (!ctx) return;
    try {
      const r = await fetch('/api/practice/pitch', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ songbookId: ctx.songbookId, variantId: ctx.variantId, level: (info && info.level) || level, pitch: s.pitch, rhythm: s.rhythm, cents: s.cents, ms: s.ms, notes: s.sung, hits: s.hits, seconds: Math.round(fr.length / 60) }),
      });
      if (r.status !== 200) return;
      const before = info;
      info = await r.json();
      const best = !before || !before.best || s.pitch > (before.best.pitch || 0);
      const reached = info.reached && !(before && before.reached);
      if (reached || (best && before && before.takes && before.takes.length)) celebrate(reached ? t('Goal reached!') : t('Personal best!'));
      renderLevels(); renderGoal(); renderHistory();
    } catch (e) { /* kept for this page only */ }
  }

  // A little party: a word, and notes floating up.
  function celebrate(word) {
    const box = document.createElement('div');
    box.className = 'take-party';
    box.innerHTML = `<strong>${word}</strong>` + Array.from({ length: 14 }, (_, i) => `<i style="left:${5 + i * 6.5}%;animation-delay:${(i % 5) * 0.12}s">${'♪♫★'[i % 3]}</i>`).join('');
    result.prepend(box);
    setTimeout(() => box.remove(), 3200);
  }
  }
})();
