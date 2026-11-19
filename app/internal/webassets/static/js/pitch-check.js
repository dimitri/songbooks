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
  if (!window.AudioContext || !navigator.mediaDevices || !window.indexedDB) return;

  // The song page: the recording's voice against the singer.
  function songPage() {
    const root = document.getElementById('audio-player');
    if (!root) return;
    const go = () => {
      const ctx = window.songbooksPracticeContext;
      create({
        mount: (panel) => root.after(panel),
        clock: () => window.songbooksReferenceClock(),
        visible: () => { const c = window.songbooksReferenceClock(); return !!(c && c.category && c.stems.includes('vocals')); },
        notesKey: (c) => `${c.variantId}:${c.category}`,
        // the factory's own helpers: the recording's voice, cut into notes
        loadNotes: async (c, progress, h) => h.notesOf(await h.referenceLine(c, progress)),
        loading: () => t('Listening to the song’s voice…'),
        loadingAt: (pc) => t('Listening to the song’s voice… %d%', pc),
        loadFailed: () => t('Couldn’t read the song’s voice.'),
        ready: () => t('Sing along: headphones on, the voice down. Play the song.'),
        key: () => (window.songbooksPlayerKey && window.songbooksPlayerKey.get()) || 0,
        seek: (tm) => window.songbooksReferenceSeek(tm),
        loadInfo: ctx ? async () => { const r = await fetch(`/api/practice/pitch?songbookId=${ctx.songbookId}&variantId=${ctx.variantId}`); return r.status === 200 ? r.json() : null; } : null,
        postTake: ctx ? async (body) => {
          const r = await fetch('/api/practice/pitch', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(Object.assign({ songbookId: ctx.songbookId, variantId: ctx.variantId }, body)) });
          return r.status === 200 ? r.json() : null;
        } : null,
      });
    };
    // The player sets its clock up as it starts: wait for it.
    let tries = 0;
    const wait = setInterval(() => {
      if (window.songbooksReferenceClock) { clearInterval(wait); go(); } else if (++tries > 40) clearInterval(wait);
    }, 250);
  }

  // create(opts) puts a checker on a page: opts.mount(panel), clock() →
  // {time, playing}, visible(), notesKey(clock), loadNotes(clock,
  // progress) → notes ({start, end, midi, afterRest}), key() (semitones
  // the notes move), seek(time), loadInfo() and postTake(body) → the
  // progress (or null), info (the progress known already), anyPitch (a
  // rhythm: entries only, any note), and its words (loading, ready…).
  // toggle: false when the page has its own button for it (start() and
  // stop() returned; onChange(on) says when it starts or stops).
  function create(opts) {

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

  // How strict, by level, from what listeners hear (Larrouy-Maestri et
  // al.: non-musicians call a note out of tune from about 20–30 cents,
  // experts from about 10). Beginner: research's "accurate" singing, half
  // a semitone (±50, Pfordresher & Demorest); intermediate: what anyone
  // would hear (±25); advanced: what a trained ear hears (±15: the
  // tracking's own error is near 10 cents, a tighter margin would fault
  // a perfect singer). On time
  // within 200, 120, 70 ms.
  // The app's; a coach sets their own (check_settings.go), applied when
  // the song's progress comes (useCoachSettings).
  const LEVELS = {
    beginner: { cents: 50, ms: 250, label: () => t('Beginner') },
    intermediate: { cents: 25, ms: 160, label: () => t('Intermediate') },
    advanced: { cents: 15, ms: 110, label: () => t('Advanced') },
  };
  function useCoachSettings(settings) {
    for (const l of (settings && settings.levels) || []) {
      const lv0 = LEVELS[l.key];
      if (!lv0) continue;
      const appLabel = lv0.appLabel || lv0.label;
      LEVELS[l.key] = { cents: l.cents, ms: l.ms, appLabel, label: l.name ? () => l.name : appLabel };
    }
  }

  // The words after a take: the coach's (check_settings.go), else the
  // app's, with {name}, {cents}, {ms}, {pitch}, {rhythm} filled in.
  const WORDS = {
    goal_reached: () => t('Goal reached! Well done, {name}.'),
    personal_best: () => t('Personal best!'),
    three_stars: () => t('Three stars: beautifully sung, {name}.'),
    keep_going: () => t('Keep going: every take counts, and the next one will be better.'),
    centre: () => t('Right in the middle of the notes.'),
    flat: () => t('A little flat ({cents} cents): aim a touch higher.'),
    sharp: () => t('A little sharp ({cents} cents): aim a touch lower.'),
    far: () => t('The notes are often far: listen to the line again, with the voice up.'),
    steady: () => t('Steady: your entries fall together.'),
    uneven: () => t('A little uneven ({ms} ms): count the rests.'),
    very_uneven: () => t('Uneven ({ms} ms): clap the rhythm, then sing it.'),
  };
  let coachSaid = false;
  function say(key, vars) {
    const mine = info && info.settings && info.settings.messages && info.settings.messages[key];
    if (mine) coachSaid = true;
    const all = Object.assign({ name: (info && info.name) || '' }, vars);
    const text = (mine || WORDS[key]()).replace(/\{(\w+)\}/g, (m, k) => (all[k] !== undefined ? String(all[k]) : m));
    const span = document.createElement('span');
    span.textContent = text.replace(/,\s*\./g, '.').replace(/\s+\./g, '.');
    return span.outerHTML;
  }
  const LEVEL_KEY = 'songbooks:pitch-level';
  const storedLevel = () => { try { return localStorage.getItem(LEVEL_KEY); } catch (e) { return null; } };

  // A note against what was sung (frames [time on the recording, midi]),
  // the sung times moved back by lag: its pitch (the median of the note's
  // steady part, octave aside) and its onset (the first frame near its
  // pitch around its start).
  function judge(note, frames, lag, key, lv) {
    const target = note.midi + key;
    const fold = (m) => { let d = m - target; d -= 12 * Math.round(d / 12); return d * 100; };
    const trim = Math.min(0.1, (note.end - note.start) * 0.25);
    let all = 0, devs = [];
    let onset = null, quiet = 0;
    for (const [tm, m] of frames) {
      const rt = tm - lag;
      // the singer comes in: a voice near the note after at least 50 ms of
      // silence, around the note's start (legato through the rest: none)
      if (note.afterRest && onset === null && rt >= note.start - 0.3 && rt <= note.start + 0.4) {
        // a rhythm (anyPitch): any note, and a consonant's short break will do
        if (isNaN(m) || (!opts.anyPitch && Math.abs(fold(m)) > 200)) quiet += 1;
        else { if (quiet >= (opts.anyPitch ? 1 : 3)) onset = rt - note.start; quiet = 0; }
      }
      if (rt < note.start + trim || rt > note.end - trim) continue; // the note's middle: its edges carry its neighbours
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
    // and only for a take mostly in tune there: a poor take's best lag is noise
    for (let l = lag0 - 0.4; l <= lag0 + 0.41; l += 0.05) { const h = hitsAt(l); if (h > best * 1.3 + 2 && h >= inTake.length * 0.5) { best = h; lag = l; } }
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
      near: near.length, // notes within twice the margin: their tendency says something
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
      <span class="hint pitch-check-status" aria-live="polite"></span>
    </div>
    <div class="pitch-check-aim">
      <span class="pitch-check-objective"></span>
      <span class="pitch-check-levels"></span>
    </div>
    <div class="pitch-check-goal" hidden></div>
    <canvas class="pitch-check-lane" height="150" hidden></canvas>
    <div class="pitch-check-result" hidden></div>
    <div class="pitch-check-history" hidden></div>`;
  opts.mount(panel);
  const $p = (sel) => panel.querySelector(sel);
  const toggle = $p('.pitch-check-toggle'), status = $p('.pitch-check-status'), lane = $p('.pitch-check-lane');
  const result = $p('.pitch-check-result'), goal = $p('.pitch-check-goal'), history = $p('.pitch-check-history'), levels = $p('.pitch-check-levels');
  const objective = $p('.pitch-check-objective');
  toggle.textContent = t('Check my singing');
  if (opts.toggle === false) toggle.hidden = true;

  // What the coach set for this song (a target, a level) and the takes so
  // far: a coaching student's song of their songbook, else nothing.
  let info = opts.info || null;
  let level = LEVELS[storedLevel()] ? storedLevel() : 'beginner';
  // What a level asks, in plain words, its margin beside them: a coach
  // may have renamed the levels and moved their margins.
  function levelWords(lv0) {
    if (opts.anyPitch) return t('on time within %d ms', lv0.ms);
    const plain = lv0.cents >= 45 ? t('within a quarter tone') : lv0.cents >= 20 ? t('as anyone hears it') : t('as a trained ear hears it');
    return `${plain} (±${lv0.cents} cents)`;
  }
  // The goal, said once (the coach's target if they set one, with the
  // level's margin), and how strict the checking is: a list whose options
  // say what each level asks; the coach's level for this song, fixed.
  function renderLevels() {
    const cur = lv();
    const target = info && (opts.anyPitch ? info.rhythmTarget : info.pitchTarget);
    objective.textContent = opts.anyPitch
      ? (target ? t('Goal: %d% of the entries within ±%d ms', target, cur.ms) : t('Goal: the entries within ±%d ms', cur.ms))
      : (target ? t('Goal: %d% of the notes within ±%d cents', target, cur.cents) : t('Goal: the notes within ±%d cents', cur.cents));
    levels.innerHTML = '';
    if (info && LEVELS[info.level]) {
      const s = document.createElement('span');
      s.className = 'pitch-level-fixed';
      s.textContent = t('Strictness: %s, set by your coach', LEVELS[info.level].label());
      s.title = levelWords(LEVELS[info.level]);
      levels.append(s);
      return;
    }
    const label = document.createElement('label');
    const sel = document.createElement('select');
    sel.className = 'pitch-check-level';
    for (const [k, lv0] of Object.entries(LEVELS)) {
      const o = document.createElement('option');
      o.value = k;
      o.textContent = `${lv0.label()} — ${levelWords(lv0)}`;
      o.selected = k === level;
      sel.append(o);
    }
    sel.addEventListener('change', () => { level = sel.value; try { localStorage.setItem(LEVEL_KEY, level); } catch (e) { /* kept for this page */ } renderLevels(); });
    label.append(t('Strictness') + ' ', sel);
    levels.append(label);
  }
  const lv = () => LEVELS[(info && LEVELS[info.level] && info.level) || level];

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
    if (opts.loadInfo) {
      try {
        const got = await opts.loadInfo();
        if (got) info = got;
      } catch (e) { /* no info: still checks, keeps nothing */ }
    }
    if (info) useCoachSettings(info.settings);
    renderLevels(); renderGoal(); renderHistory();
  }
  if (info) useCoachSettings(info.settings);
  renderLevels();

  // Shown when the playing recording has its voice apart.
  let infoAsked = false;
  const refresh = () => {
    panel.hidden = !opts.visible();
    if (!panel.hidden && !infoAsked) { infoAsked = true; loadInfo(); }
  };
  refresh();
  setInterval(refresh, 2000);

  let on = false, line = null, notes = [], lineKey = '', mic = null, ac = null, analyser = null, raf = 0;
  let frames = []; // the take: [time on the recording, midi]
  let wasPlaying = false, lag0 = 0.15;

  async function start() {
    const clock = opts.clock();
    on = true;
    toggle.textContent = t('Stop checking');
    toggle.classList.add('is-on');
    if (opts.onChange) opts.onChange(true);
    result.hidden = true;
    const key = opts.notesKey(clock);
    if (!line || lineKey !== key) {
      status.textContent = opts.loading();
      try {
        notes = await opts.loadNotes(clock, (p) => { status.textContent = opts.loadingAt(Math.round(p * 100)); }, { notesOf, referenceLine });
        line = true;
        lineKey = key;
      } catch (e) {
        console.warn('pitch-check: the line to sing: ' + (e && (e.stack || e.message) || e));
        status.textContent = opts.loadFailed();
        stop();
        return;
      }
    }
    try {
      // an iPhone keeps playing while it listens (and with the ring switch on silent)
      try { if (navigator.audioSession) navigator.audioSession.type = 'play-and-record'; } catch (e) { /* not this browser */ }
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
    status.textContent = opts.ready();
    frames = [];
    loop();
  }

  function stop() {
    on = false;
    toggle.textContent = t('Check my singing');
    toggle.classList.remove('is-on');
    if (opts.onChange) opts.onChange(false);
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
    const clock = opts.clock();
    const key = opts.key();
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
    const w = lane.clientWidth, h = lane.clientHeight || 150, dpr = window.devicePixelRatio || 1;
    if (lane.width !== Math.round(w * dpr) || lane.height !== Math.round(h * dpr)) { lane.width = Math.round(w * dpr); lane.height = Math.round(h * dpr); }
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
  const escapeHTML = (x) => String(x).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));
  const clockTime = (s) => `${Math.floor(s / 60)}:${String(Math.floor(s % 60)).padStart(2, '0')}`;
  const stars = (p, target) => { const goalAt = target || 70; return p >= goalAt ? 3 : p >= goalAt * 0.75 ? 2 : p >= goalAt * 0.4 ? 1 : 0; };
  async function endTake() {
    const fr = frames;
    frames = [];
    live.clear();
    if (fr.length < 240 || !notes.length) return; // under ~4 s of singing: no score
    const key = opts.key();
    const s = score(notes, fr, key, lv(), lag0);
    if (s.sung < 4) return;
    const target = info && info.pitchTarget;
    const n = opts.anyPitch ? stars(s.rhythm, info && info.rhythmTarget) : stars(s.pitch, target);
    coachSaid = false;
    const vars = { cents: Math.abs(s.cents), ms: s.ms, pitch: s.pitch, rhythm: s.rhythm };
    const tendency = opts.anyPitch ? '' : s.near < s.sung / 2 ? say('far', vars) : Math.abs(s.cents) < 10 ? say('centre', vars) : say(s.cents < 0 ? 'flat' : 'sharp', vars);
    const half = lv().ms / 2;
    const timing = s.timed < 3 ? '' : say(s.ms <= half ? 'steady' : s.ms <= lv().ms ? 'uneven' : 'very_uneven', vars);
    const cheer = n === 3 ? say('three_stars', vars) : n === 0 ? say('keep_going', vars) : '';
    result.innerHTML = `
      <div class="take-stars" aria-label="${t('%d stars out of 3', n)}">${[0, 1, 2].map((i) => `<span class="${i < n ? 'is-on' : ''}">★</span>`).join('')}</div>
      <div class="take-scores">
        ${opts.anyPitch ? '' : `<div><strong>${s.pitch}%</strong><span>${t('in tune')}</span><small>${t('%d of %d notes', s.hits, s.sung)}</small></div>`}
        <div><strong>${s.rhythm}%</strong><span>${t('on time')}</span><small>${t('%d of %d notes', s.onTime, s.timed)}</small></div>
      </div>
      <p class="take-words">${cheer ? cheer + ' ' : ''}${tendency} ${timing}${coachSaid && info.coach ? ` <span class="take-coach">— ${escapeHTML(info.coach)}</span>` : ''}</p>`;
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
        b.addEventListener('click', () => opts.seek(Math.max(0, s0 - 2)));
        q.append(i ? ', ' : '', b);
      });
      result.append(q);
    }
    result.hidden = false;
    if (!opts.postTake) return;
    try {
      const got = await opts.postTake({ level: (info && info.level) || level, pitch: opts.anyPitch ? 0 : s.pitch, rhythm: s.rhythm, cents: s.cents, ms: s.ms, notes: s.sung, hits: s.hits, seconds: Math.round(fr.length / 60) });
      if (!got) return;
      const before = info;
      info = got;
      useCoachSettings(info.settings);
      const best = !before || !before.best || s.pitch > (before.best.pitch || 0);
      const reached = info.reached && !(before && before.reached);
      if (reached || (best && before && before.takes && before.takes.length)) celebrate(reached ? say('goal_reached', vars) : say('personal_best', vars));
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
    refresh();
    return { refresh, start: () => { if (!on) start(); }, stop: () => { if (on) stop(); } };
  }

  window.songbooksPitchCheck = { create };
  songPage();
})();
