// Reference-audio player: waveform (hand-drawn via Web Audio decodeAudioData
// + <canvas>, no CDN library — see app/docs/mp3-reference-audio.md §3),
// transport controls, and section markers synced against the song's own
// structure strip (#song-structure-strip's .structure-seg buttons, the
// SAME elements/CSS classes the metronome's own playback highlight uses
// in song.js — reused here, not reimplemented, just driven by
// audio.currentTime instead of the BPM clock).
//
// Two reference CATEGORIES per song ("a personal choice of reference
// version (yt video, band recording: two categories only)" — store.
// AudioCategory's own doc comment): "youtube" and "band". Upload/replace/
// remove controls for both live in the Reference drawer (wired at the
// bottom of this file); THIS player is playback-only, with a toggle
// between categories when a song has both. Which category is selected is
// a per-browser preference (localStorage, keyed by book/stem), not song
// data — two listeners can prefer different reference versions of the
// same song without fighting over a shared setting.

// Shared by audio-player.js and mixer.js: one cached download per
// (song, category, size) — the size changes when a recording is replaced.
window.songbooksAudioBytes = window.songbooksAudioBytes || (function () {
  const cache = new Map();
  return function (variantId, category, meta) {
    const url = `/song/${variantId}/audio.mp3?category=${category}&v=${(meta && meta.byteSize) || ''}`;
    if (!cache.has(url)) {
      cache.set(url, fetch(url).then((r) => {
        if (!r.ok) throw new Error('HTTP ' + r.status);
        return r.arrayBuffer();
      }).catch((e) => { cache.delete(url); throw e; }));
    }
    return cache.get(url);
  };
})();

// The bytes Web Audio's decodeAudioData will take. Some MP3s — Guitar
// Pro embeds them like this — open with a few KB of zero padding before
// the first frame: <audio> skips it, decodeAudioData rejects the whole
// file (no waveform, no auto-align). Padding is not audio, so dropping it
// shifts nothing in time.
window.songbooksDecodable = function (buf) {
  const bytes = new Uint8Array(buf);
  if (bytes.length === 0 || bytes[0] !== 0) return buf;
  let i = 0;
  const limit = Math.min(bytes.length, 1 << 20);
  while (i < limit && bytes[i] === 0) i++;
  return i < limit ? buf.slice(i) : buf;
};

(function () {
  const root = document.getElementById('audio-player');
  if (!root) return; // not on a page with the player
  const variantId = root.dataset.variantId;
  const canEdit = root.dataset.canEdit === 'true';
  // "-choice": only an explicit toggle click is remembered. The old key
  // also saved whatever category was auto-picked on load, so a song that
  // once had only a band clip kept preferring it after a real Guitar Pro
  // recording was added.
  const prefKey = `songbooks:audio-category-choice:${variantId}`;
  const $ = (id) => document.getElementById(id);

  const categoryToggle = $('audio-category-toggle');
  const emptyState = $('audio-empty-state');
  const playerBody = $('audio-player-body');
  const canvas = $('audio-waveform');
  const playedCanvas = $('audio-waveform-played');
  const playedClip = $('audio-played');
  const waveEl = $('audio-wave');
  const rulerEl = $('audio-ruler');
  const rulerMarks = $('audio-ruler-marks');
  const cycleEl = $('audio-cycle');
  const cycleShade = $('audio-cycle-shade');
  const bargridEl = $('audio-bargrid');
  const playhead = $('audio-playhead');
  const hoverEl = $('audio-hover');
  const hoverLabel = $('audio-hover-label');
  const playBtn = $('audio-play-btn');
  const lcdPos = $('audio-lcd-pos');
  const lcdSection = $('audio-lcd-section');
  const timeLabel = $('audio-time-label');
  const optsBtn = $('audio-opts-btn');
  const optsEl = $('audio-opts');
  const cycleBtn = $('audio-cycle-btn');
  const countInBtn = $('audio-countin-btn');
  const countInLabel = $('audio-countin-label');
  const clickBtn = $('audio-click-btn');
  const speedSel = $('audio-speed');
  const speedBpm = $('audio-speed-bpm');
  const trainerBtn = $('audio-trainer-btn');
  const trainerLabel = $('audio-trainer-label');
  const iconPlay = playBtn ? playBtn.innerHTML : '▶';
  const iconPause = '<svg class="icon" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><rect x="5" y="4" width="3.5" height="12" rx="1"/><rect x="11.5" y="4" width="3.5" height="12" rx="1"/></svg>';

  const audio = new Audio();
  audio.preload = 'metadata';
  // Slowing down keeps the pitch (the browsers' default, made explicit).
  audio.preservesPitch = true;
  audio.mozPreservesPitch = true;
  audio.webkitPreservesPitch = true;
  let peaks = null; // Float32Array of per-bucket peak amplitudes, 0..1
  let duration = 0;
  let categories = {}; // {gp|youtube|band: {markers, barSyncPoints, byteSize}}
  let activeCategory = null;

  // ---- The song: tempo, metre, sections (from the page) -------------------
  const transportEl = $('transport');
  const bpm = transportEl ? parseFloat(transportEl.dataset.bpm) || 0 : 0;
  const beatsPerBar = transportEl ? parseInt(transportEl.dataset.beatsPerBar, 10) || 4 : 4;
  const timeNum = transportEl ? parseInt(transportEl.dataset.timeNum, 10) || 4 : 4;
  const timeDen = transportEl ? parseInt(transportEl.dataset.timeDen, 10) || 4 : 4;
  // 6/8, 9/8, 12/8: each main pulse is a dotted quarter, clicked with its
  // two softer eighths like the metronome does.
  const isCompound = timeDen === 8 && [6, 9, 12].includes(timeNum);
  const secondsPerBar = bpm > 0 ? (60 / bpm) * beatsPerBar : 0;
  // A song opening with a pickup numbers it 0, so the first full bar is 1.
  const hasPickup = () => !!document.querySelector('#sheet .bar-pickup');
  const barLabel = (b) => String(hasPickup() ? b : b + 1);

  function sections() {
    const out = [];
    document.querySelectorAll('.sheet-section[data-start-bar]').forEach((el) => {
      const startBar = parseInt(el.dataset.startBar, 10);
      const barCount = parseInt(el.dataset.barCount, 10) || 0;
      if (Number.isNaN(startBar) || barCount <= 0) return;
      out.push({ index: parseInt(el.dataset.sectionIndex, 10), label: el.dataset.label || '', startBar, barCount });
    });
    return out.sort((a, b) => a.startBar - b.startBar);
  }
  const totalBars = () => sections().reduce((m, s) => Math.max(m, s.startBar + s.barCount), 0);
  const sectionAt = (bar) => sections().find((s) => bar >= s.startBar && bar < s.startBar + s.barCount) || null;

  // ---- Practice settings, remembered per song in this browser -------------
  const optsKey = `songbooks:player:${variantId}`;
  const prefs = {
    speed: 1, countIn: 0, countInEach: false,
    click: false, clickSound: 'click', clickVolume: 70,
    trainer: { on: false, start: 60, end: 100, step: 10, every: 2 },
    cycle: null, // {from, to, on}: bars [from, to)
  };
  try {
    const saved = JSON.parse(localStorage.getItem(optsKey) || '{}');
    Object.assign(prefs, saved, { trainer: { ...prefs.trainer, ...(saved.trainer || {}), on: false }, cycle: null });
  } catch (e) { /* defaults */ }
  // A loop — the cycle range, and the trainer that runs on it — is for
  // this sitting only: every visit starts playing the song straight.
  const savePrefs = () => {
    const keep = { ...prefs, cycle: null, trainer: { ...prefs.trainer, on: false } };
    if (prefs.trainer.on) keep.speed = 1;
    try { localStorage.setItem(optsKey, JSON.stringify(keep)); } catch (e) { /* ignore */ }
  };

  function fmtTime(s) {
    if (!isFinite(s) || s < 0) s = 0;
    const m = Math.floor(s / 60), sec = Math.floor(s % 60);
    return `${m}:${String(sec).padStart(2, '0')}`;
  }

  // ---- Waveform: decode once per category switch, draw peaks into a
  // fixed-width bucket array (independent of canvas pixel width, which
  // can change on resize) so a resize just re-renders from cached peaks,
  // no re-decode. ------------------------------------------------------
  // One download per recording, shared with the Mixer (mixer.js): the
  // bytes feed both the <audio> element (as a blob URL) and the waveform.
  // Streaming the URL into <audio> AND fetching it again for the waveform
  // left half-read media connections open, exhausting the browser's six
  // connections per host until every other request on the page stalled.
  async function decodeWaveform() {
    peaks = null;
    const cat = activeCategory;
    let bytes;
    try { bytes = await window.songbooksAudioBytes(variantId, cat, categories[cat]); } catch (e) { return; }
    if (cat !== activeCategory) return;
    if (audio.dataset.blobFor !== cat) {
      if (audio.src.startsWith('blob:')) URL.revokeObjectURL(audio.src);
      audio.src = URL.createObjectURL(new Blob([bytes], { type: 'audio/mpeg' }));
      audio.dataset.blobFor = cat;
      audio.playbackRate = prefs.speed;
    }
    const arrayBuffer = window.songbooksDecodable(bytes.slice(0));
    const ctx = new (window.AudioContext || window.webkitAudioContext)();
    let audioBuffer;
    try {
      audioBuffer = await ctx.decodeAudioData(arrayBuffer);
    } catch (e) {
      return; // unsupported/corrupt file — the player still works via <audio>, just no waveform
    } finally {
      ctx.close();
    }
    const channel = audioBuffer.getChannelData(0);
    const buckets = 800; // fixed resolution, downsampled further at draw time if canvas is narrower
    const samplesPerBucket = Math.floor(channel.length / buckets) || 1;
    const out = new Float32Array(buckets);
    for (let i = 0; i < buckets; i++) {
      let max = 0;
      const start = i * samplesPerBucket;
      const end = Math.min(start + samplesPerBucket, channel.length);
      for (let j = start; j < end; j++) {
        const v = Math.abs(channel[j]);
        if (v > max) max = v;
      }
      out[i] = max;
    }
    peaks = out;
    detectBars(audioBuffer, cat);
    rebuildGrid();
    drawWaveform();
  }

  // ---- Unaligned recordings: no per-bar sync points and no section
  // markers. Rather than leave the chart without a playing bar, find the
  // count-in and downbeats the way the Mixer's Auto-align does
  // (mixer-autoalign.js) and follow that, in memory only: saving an
  // alignment stays an explicit choice in the Mixer.
  let detected = null; // [{barIndex, timeSeconds}] for the active category
  function detectBars(audioBuffer, cat) {
    detected = null;
    const meta = categories[cat] || {};
    if ((meta.barSyncPoints && meta.barSyncPoints.length) || (meta.markers && meta.markers.length)) return;
    if (!window.SongbooksAutoAlign || !transportEl) return;
    const total = totalBars();
    if (!total || !bpm) return;
    try {
      const f = Math.max(1, Math.round(audioBuffer.sampleRate / 11025));
      const c0 = audioBuffer.getChannelData(0);
      const c1 = audioBuffer.numberOfChannels > 1 ? audioBuffer.getChannelData(1) : c0;
      const x = new Float32Array(Math.floor(audioBuffer.length / f));
      for (let i = 0, j = 0; i < x.length; i++) {
        let sum = 0;
        for (let k = 0; k < f; k++, j++) sum += c0[j] + c1[j];
        x[i] = sum / (2 * f);
      }
      const ticksPerBar = (timeNum * 3840) / timeDen;
      const barTicks = [];
      for (let b = 0; b <= total; b++) barTicks.push(b * ticksPerBar);
      const res = window.SongbooksAutoAlign.analyze({ x, sr: audioBuffer.sampleRate / f }, {
        tempo: bpm, barTicks, onsets: [], barChords: [],
        firstFull: hasPickup() ? 1 : 0,
      });
      if (cat === activeCategory && res && res.barTimes) detected = res.barTimes.map((t, i) => ({ barIndex: i, timeSeconds: t }));
    } catch (e) { /* no highlight beats a wrong one */ }
  }

  // ---- The bar grid: grid[b] is when bar b starts in the recording, for
  // every bar the recording covers. From the per-bar sync points (a Guitar
  // Pro import, an auto-align, or the in-memory detection above), else
  // the section markers plus the song's tempo. Everything bar-based —
  // the LCD, the cycle range, the click, bar skipping — reads it.
  let grid = [];
  const hasBars = () => grid.length > 1;
  function currentMarkers() { return (categories[activeCategory] && categories[activeCategory].markers) || []; }
  function rebuildGrid() {
    grid = [];
    if (!duration) return refreshTimeline();
    const synced = (categories[activeCategory] && categories[activeCategory].barSyncPoints) || [];
    const pts = (synced.length ? synced : detected || []).slice().sort((a, b) => a.barIndex - b.barIndex);
    let at = null;
    if (pts.length > 1) {
      at = (b) => {
        let i = 0;
        while (i < pts.length - 2 && pts[i + 1].barIndex <= b) i++;
        const p = pts[i], q = pts[i + 1];
        return p.timeSeconds + ((b - p.barIndex) / (q.barIndex - p.barIndex)) * (q.timeSeconds - p.timeSeconds);
      };
    } else if (secondsPerBar) {
      const secs = sections();
      const marks = currentMarkers()
        .map((m) => ({ t: m.timeSeconds, start: (secs.find((s) => s.index === m.sectionIndex) || {}).startBar }))
        .filter((m) => m.start !== undefined)
        .sort((a, b) => a.start - b.start);
      if (marks.length) {
        at = (b) => {
          let m = marks[0];
          for (const k of marks) if (k.start <= b) m = k;
          return m.t + (b - m.start) * secondsPerBar;
        };
      }
    }
    if (at) {
      for (let b = 0; b < 5000; b++) {
        const t = at(b);
        grid.push(t);
        if (t > duration) break;
      }
    }
    refreshTimeline();
  }
  function timeAtBar(b) {
    if (!hasBars()) return null;
    const n = grid.length - 1;
    const i = Math.max(0, Math.min(n - 1, Math.floor(b)));
    return grid[i] + (b - i) * (grid[i + 1] - grid[i]);
  }
  function barAtTime(t) {
    if (!hasBars()) return null;
    let lo = 0, hi = grid.length - 1;
    if (t < grid[0]) hi = 1;
    else if (t >= grid[hi]) lo = hi - 1;
    else {
      while (hi - lo > 1) {
        const mid = (lo + hi) >> 1;
        if (grid[mid] <= t) lo = mid; else hi = mid;
      }
    }
    return lo + (t - grid[lo]) / (grid[lo + 1] - grid[lo]);
  }
  // Where the recording is in the chart, as a fractional performed bar
  // (2.5 = halfway through bar index 2); song.js reads it to highlight the
  // playing bar and drive the tab cursor. null when the recording isn't
  // playing or hasn't been started.
  window.songbooksReferencePosition = () => {
    if (!duration || (audio.paused && !countingIn && audio.currentTime === 0)) return null;
    const bar = barAtTime(audio.currentTime);
    return bar === null ? null : { bar, playing: !audio.paused || countingIn };
  };

  // ---- Drawing ------------------------------------------------------------
  const pctOf = (t) => (duration ? (t / duration) * 100 : 0);
  function paint(cv, color) {
    const ctx2 = cv.getContext('2d');
    const dpr = window.devicePixelRatio || 1;
    const w = waveEl.clientWidth, h = waveEl.clientHeight;
    if (!w || !h) return;
    cv.style.width = `${w}px`;
    cv.width = w * dpr; cv.height = h * dpr;
    ctx2.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx2.clearRect(0, 0, w, h);
    ctx2.fillStyle = color;
    const mid = h / 2;
    // Rounded bars with a gap, SoundCloud-style, about one per 3px.
    const n = Math.max(1, Math.floor(w / 3));
    const step = peaks.length / n;
    for (let i = 0; i < n; i++) {
      let p = 0;
      for (let j = Math.floor(i * step); j < Math.floor((i + 1) * step); j++) p = Math.max(p, peaks[j] || 0);
      const amp = Math.max(p, 0.03) * (mid - 1);
      ctx2.fillRect(i * 3, mid - amp, 2, amp * 2);
    }
  }
  function drawWaveform() {
    if (!peaks || !canvas) return;
    const style = getComputedStyle(root);
    paint(canvas, style.getPropertyValue('--waveform-color').trim() || '#bbb');
    if (playedCanvas) paint(playedCanvas, style.getPropertyValue('--waveform-played').trim() || style.getPropertyValue('--accent').trim() || '#af4e39');
  }

  // Faint ticks for bars, stronger ones where a section starts; the ruler
  // above carries bar numbers (thinned out when bars get narrow) and the
  // section names.
  function refreshTimeline() {
    if (bargridEl) bargridEl.innerHTML = '';
    if (rulerMarks) rulerMarks.innerHTML = '';
    if (!duration || !hasBars()) { renderCycle(); updateButtons(); wireBarClickToPlay(); return; }
    const starts = new Map(sections().map((s) => [s.startBar, s]));
    const width = waveEl.clientWidth || 600;
    const barPx = (width * (grid[Math.min(grid.length - 1, 2)] - grid[1] || secondsPerBar)) / duration;
    const every = [1, 2, 4, 8, 16].find((k) => barPx * k >= 26) || 32;
    const chartBars = totalBars();
    grid.forEach((t, b) => {
      // A pickup bar may start a moment before the recording does.
      if (t < 0 && b === 0) t = 0;
      if (t < 0 || t > duration) return;
      const sec = starts.get(b);
      const tick = document.createElement('div');
      tick.className = 'audio-bargrid-tick' + (sec ? ' section' : '');
      tick.style.left = `${pctOf(t)}%`;
      bargridEl.appendChild(tick);
      // Numbers only over the chart's own bars; the recording may run on.
      const numbered = b < chartBars && b % every === (hasPickup() ? 0 : every - 1);
      const label = sec ? sec.label : (numbered ? barLabel(b) : '');
      if (label) {
        const m = document.createElement('span');
        m.className = sec ? 'audio-ruler-section' : 'audio-ruler-bar';
        m.style.left = `${pctOf(t)}%`;
        m.textContent = label;
        if (sec) { m.dataset.startBar = sec.startBar; m.dataset.barCount = sec.barCount; m.title = `${sec.label} — click to cycle it, Shift-click to extend the cycle`; }
        rulerMarks.appendChild(m);
      }
    });
    renderCycle();
    updateButtons();
    wireBarClickToPlay();
  }

  function renderCycle() {
    if (typeof markCycleInChart === 'function') markCycleInChart();
    const c = prefs.cycle;
    const show = c && hasBars() && duration;
    if (cycleEl) cycleEl.hidden = !show;
    if (cycleShade) cycleShade.hidden = !(show && c.on);
    if (!show) return;
    const l = pctOf(timeAtBar(c.from)), r = pctOf(timeAtBar(c.to));
    for (const el of [cycleEl, cycleShade]) {
      el.style.left = `${Math.max(0, l)}%`;
      el.style.width = `${Math.max(0.3, Math.min(100, r) - Math.max(0, l))}%`;
    }
    cycleEl.classList.toggle('on', !!c.on);
    cycleEl.title = `cycle: bars ${barLabel(c.from)}–${barLabel(c.to - 1)} — click to switch ${c.on ? 'off' : 'on'}, drag to move, drag an edge to resize`;
  }

  function renderPosition() {
    if (!duration) return;
    const t = audio.currentTime;
    const pct = Math.max(0, Math.min(100, pctOf(t)));
    if (playhead) playhead.style.left = `${pct}%`;
    if (playedClip) playedClip.style.width = `${pct}%`;
    if (timeLabel) timeLabel.textContent = `${fmtTime(t)} / ${fmtTime(duration)}`;
    const pos = barAtTime(t);
    if (pos === null) {
      lcdPos.textContent = fmtTime(t);
      lcdSection.textContent = '';
      return;
    }
    const b = Math.floor(pos);
    const beat = Math.min(beatsPerBar - 1, Math.floor((pos - b) * beatsPerBar));
    lcdPos.textContent = b < 0 ? '–' : `${barLabel(b)}.${beat + 1}`;
    const sec = sectionAt(b);
    lcdSection.textContent = sec ? sec.label : '';
  }

  function updateButtons() {
    const playing = !audio.paused || countingIn;
    if (playBtn) {
      const want = playing ? iconPause : iconPlay;
      if (playBtn.dataset.state !== String(playing)) { playBtn.innerHTML = want; playBtn.dataset.state = String(playing); }
      playBtn.classList.toggle('active', playing);
    }
    const press = (btn, on) => { if (btn) { btn.classList.toggle('active', !!on); btn.setAttribute('aria-pressed', on ? 'true' : 'false'); } };
    press(cycleBtn, prefs.cycle && prefs.cycle.on);
    press(countInBtn, prefs.countIn > 0);
    press(clickBtn, prefs.click);
    press(trainerBtn, prefs.trainer.on);
    if (cycleBtn) cycleBtn.disabled = !hasBars();
    if (countInLabel) countInLabel.textContent = prefs.countIn ? `${prefs.countIn} bar${prefs.countIn > 1 ? 's' : ''}` : 'Count-in';
    if (trainerLabel) trainerLabel.textContent = prefs.trainer.on ? `Trainer ${Math.round(prefs.speed * 100)}% → ${prefs.trainer.end}%` : 'Trainer';
    const meter = $('audio-trainer-meter');
    if (meter) {
      const tr = prefs.trainer;
      const span = Math.max(1, tr.end - tr.start);
      meter.style.width = tr.on ? `${Math.max(4, Math.min(100, ((prefs.speed * 100 - tr.start) / span) * 100))}%` : '0';
    }
    renderTrainer();
  }

  // ---- Speed (pitch kept) ---------------------------------------------------
  const SPEEDS = [50, 60, 70, 75, 80, 90, 100, 110, 125];
  function setSpeed(v, save = true) {
    const pct = Math.round(Math.max(25, Math.min(150, v * 100)));
    prefs.speed = pct / 100;
    audio.playbackRate = prefs.speed;
    if (speedSel) {
      const opts = [...new Set([...SPEEDS, pct])].sort((a, b) => a - b);
      if (speedSel.options.length !== opts.length || ![...speedSel.options].some((o) => +o.value === pct)) {
        speedSel.innerHTML = '';
        for (const p of opts) speedSel.add(new Option(`${p}%`, String(p)));
      }
      speedSel.value = String(pct);
    }
    if (speedBpm) speedBpm.textContent = bpm ? `${Math.round(bpm * prefs.speed)} bpm` : '';
    resetClicks();
    if (save) savePrefs();
    updateButtons();
  }
  speedSel?.addEventListener('change', () => setSpeed(+speedSel.value / 100));

  // ---- Click: the metronome's own sounds (clock.js), placed on the
  // recording's bar grid so it follows a live tempo, not a fixed BPM ----
  /* global Metronome */
  const clicker = typeof Metronome === 'function' ? new Metronome({}) : null;
  let clicksUntil = null; // recording time up to which clicks are scheduled
  const resetClicks = () => { clicksUntil = null; };
  function clickAt(when, beatInBar, soft) {
    if (!clicker) return;
    clicker.sound = prefs.clickSound;
    clicker._click(when, beatInBar === 0 && !soft, soft, prefs.clickVolume / 70);
  }
  // Every beat (and compound-metre eighth) with a recording time in [a, b).
  function beatsBetween(a, b) {
    const out = [];
    const first = Math.floor(barAtTime(a));
    for (let bar = first; bar < first + 64; bar++) {
      const t0 = timeAtBar(bar), t1 = timeAtBar(bar + 1);
      if (t0 >= b) break;
      const beatDur = (t1 - t0) / beatsPerBar;
      for (let k = 0; k < beatsPerBar; k++) {
        const subs = isCompound ? 3 : 1;
        for (let s = 0; s < subs; s++) {
          const t = t0 + (k + s / subs) * beatDur;
          if (t >= a && t < b) out.push({ t, k, soft: s > 0 });
        }
      }
    }
    return out;
  }
  function scheduleClicks() {
    if (!prefs.click || !clicker || !hasBars() || audio.paused) return;
    const ctx = clicker._ensureCtx();
    const now = audio.currentTime, rate = audio.playbackRate || 1;
    const until = now + 0.12 * rate;
    const from = clicksUntil === null ? now : Math.max(clicksUntil, now);
    for (const beat of beatsBetween(from, until)) clickAt(ctx.currentTime + (beat.t - now) / rate, beat.k, beat.soft);
    clicksUntil = until;
  }

  // ---- Transport ----------------------------------------------------------
  let countingIn = false;
  let countInTimer = null;
  let passes = 0; // completed cycle passes since play, for the trainer

  function cycleRange() {
    const c = prefs.cycle;
    if (!c || !c.on || !hasBars()) return null;
    return { t0: timeAtBar(c.from), t1: timeAtBar(c.to) };
  }

  // Counts in prefs.countIn bars at the tempo of the bar about to play
  // (and the current speed), then starts the recording at t, on the beat.
  function countInThen(t) {
    const ctx = clicker ? clicker._ensureCtx() : null;
    const bars = prefs.countIn;
    if (!ctx || !bars || !hasBars()) { startAt(t); return; }
    const b = Math.round(barAtTime(Math.max(t, grid[0])));
    const rate = audio.playbackRate || 1;
    const barDur = (timeAtBar(b + 1) - timeAtBar(b)) / rate;
    const beatDur = barDur / beatsPerBar;
    const t0 = ctx.currentTime + 0.12;
    for (let i = 0; i < bars * beatsPerBar; i++) {
      clickAt(t0 + i * beatDur, i % beatsPerBar, false);
      if (isCompound) for (let s = 1; s < 3; s++) clickAt(t0 + (i + s / 3) * beatDur, 1, true);
    }
    // A start before 0 (a pickup bar starting before the recording does)
    // waits that much longer, from the recording's first sample.
    const startWall = t0 + bars * barDur + Math.max(0, -t) / rate;
    audio.currentTime = Math.max(0, t);
    countingIn = true;
    clearTimeout(countInTimer);
    countInTimer = setTimeout(() => {
      countingIn = false;
      startAt(Math.max(0, t), true);
    }, Math.max(0, (startWall - ctx.currentTime) * 1000 - 15));
    updateButtons();
  }
  function startAt(t, already) {
    if (!already) audio.currentTime = Math.max(0, t);
    resetClicks();
    document.dispatchEvent(new CustomEvent('songbooks:reference-playing'));
    audio.play().catch(() => {});
  }

  function play() {
    if (!duration || countingIn) return;
    const cyc = cycleRange();
    let t = audio.currentTime;
    if (cyc && (t < cyc.t0 - 0.05 || t >= cyc.t1 - 0.05)) t = cyc.t0;
    if (prefs.trainer.on && cyc) { passes = 0; setSpeed(prefs.trainer.start / 100); }
    if (prefs.countIn && hasBars()) {
      // Count into the bar being played: from its downbeat.
      if (!cyc) t = timeAtBar(Math.floor(barAtTime(t) + 0.02));
      countInThen(t);
    } else {
      startAt(t);
    }
  }
  function pause() {
    clearTimeout(countInTimer);
    countingIn = false;
    audio.pause();
    updateButtons();
  }
  const isPlaying = () => !audio.paused || countingIn;
  function seek(t, keepPlaying = true) {
    if (!duration) return;
    const wasCounting = countingIn;
    if (wasCounting) pause();
    audio.currentTime = Math.max(0, Math.min(duration - 0.05, t));
    resetClicks();
    renderPosition();
    if (wasCounting && keepPlaying) play();
  }
  function seekToBar(b) {
    const t = timeAtBar(b);
    if (t !== null) seek(t);
  }

  // The cycle's end: back to its start — after a count-in when asked to
  // count in every pass — speeding up by the trainer's step every N passes.
  function onCycleEnd(cyc) {
    passes++;
    const tr = prefs.trainer;
    if (tr.on && passes % Math.max(1, tr.every) === 0 && prefs.speed * 100 < tr.end) {
      setSpeed(Math.min(tr.end, prefs.speed * 100 + tr.step) / 100, false);
    }
    if (prefs.countIn && prefs.countInEach) {
      audio.pause();
      countInThen(cyc.t0);
    } else {
      audio.currentTime = cyc.t0;
      resetClicks();
    }
    updateButtons();
  }

  function tick() {
    if (!audio.paused && !countingIn) {
      const cyc = cycleRange();
      if (cyc && audio.currentTime >= cyc.t1 - 0.012) onCycleEnd(cyc);
      scheduleClicks();
    }
    renderPosition();
  }
  // A timer, not requestAnimationFrame: the cycle wrap and the click
  // scheduler must keep running when the tab is in the background.
  setInterval(tick, 15);

  playBtn?.addEventListener('click', () => (isPlaying() ? pause() : play()));
  $('audio-start-btn')?.addEventListener('click', () => {
    const cyc = cycleRange();
    seek(cyc ? cyc.t0 : (hasBars() ? Math.max(0, grid[0]) : 0));
  });
  function stepBar(dir) {
    if (!hasBars()) { seek(audio.currentTime + dir * 5); return; }
    const cur = barAtTime(audio.currentTime);
    seekToBar(dir < 0 ? Math.max(0, Math.ceil(cur - 0.05) - 1) : Math.floor(cur) + 1);
  }
  function stepSection(dir) {
    if (!hasBars()) { seek(audio.currentTime + dir * 15); return; }
    const cur = barAtTime(audio.currentTime);
    const starts = sections().map((s) => s.startBar);
    const target = dir < 0 ? starts.filter((b) => b < cur - 0.05).pop() : starts.find((b) => b > cur + 0.01);
    if (target !== undefined) seekToBar(target);
    else if (dir < 0) seekToBar(0);
  }
  $('audio-prev-btn')?.addEventListener('click', (e) => (e.altKey ? stepSection(-1) : stepBar(-1)));
  $('audio-next-btn')?.addEventListener('click', (e) => (e.altKey ? stepSection(1) : stepBar(1)));
  audio.addEventListener('play', updateButtons);
  audio.addEventListener('pause', updateButtons);
  audio.addEventListener('ended', () => { passes = 0; updateButtons(); });
  audio.addEventListener('loadedmetadata', () => {
    duration = audio.duration;
    audio.playbackRate = prefs.speed;
    rebuildGrid();
    renderPosition();
  });

  // ---- Options ------------------------------------------------------------
  function toggleCycle() {
    if (!hasBars()) return;
    if (!prefs.cycle) {
      // No range yet: cycle the section being played, as Logic cycles a
      // marker dragged into the ruler.
      const sec = sectionAt(Math.max(0, Math.floor(barAtTime(audio.currentTime))));
      prefs.cycle = sec ? { from: sec.startBar, to: sec.startBar + sec.barCount, on: true } : { from: 0, to: Math.min(4, grid.length - 1), on: true };
    } else {
      prefs.cycle.on = !prefs.cycle.on;
    }
    passes = 0;
    savePrefs();
    renderCycle();
    updateButtons();
  }
  cycleBtn?.addEventListener('click', toggleCycle);
  countInBtn?.addEventListener('click', () => {
    prefs.countIn = (prefs.countIn + 1) % 3;
    savePrefs();
    updateButtons();
  });
  function toggleClick() {
    prefs.click = !prefs.click;
    resetClicks();
    savePrefs();
    updateButtons();
  }
  clickBtn?.addEventListener('click', toggleClick);
  // ---- Option panels: one open at a time, under the control bar -------
  const pops = [...document.querySelectorAll('#audio-player .audio-pop')];
  function openPop(id, opener) {
    const el = $(id);
    const show = el.hidden;
    pops.forEach((p) => { p.hidden = true; });
    document.querySelectorAll('#audio-player [data-pop]').forEach((b) => b.setAttribute('aria-expanded', 'false'));
    if (!show) return;
    el.hidden = false;
    opener.setAttribute('aria-expanded', 'true');
    // Under its button where there's room; full width on a phone (CSS).
    const wrap = root.getBoundingClientRect(), r = opener.getBoundingClientRect();
    el.style.setProperty('--pop-top', `${r.bottom - wrap.top + 6}px`);
    el.style.setProperty('--pop-left', `${Math.max(8, Math.min(wrap.width - el.offsetWidth - 8, r.right - wrap.left - el.offsetWidth))}px`);
    if (id === 'audio-pop-trainer') renderTrainer();
  }
  document.querySelectorAll('#audio-player [data-pop]').forEach((b) => b.addEventListener('click', () => openPop(b.dataset.pop, b)));
  document.addEventListener('pointerdown', (e) => {
    if (e.target.closest('#audio-player .audio-pop, #audio-player [data-pop]')) return;
    pops.forEach((p) => { p.hidden = true; });
  });
  document.addEventListener('keydown', (e) => { if (e.key === 'Escape') pops.forEach((p) => { p.hidden = true; }); });

  function wireSeg(id, get, set) {
    const seg = $(id);
    if (!seg) return () => {};
    const sync = () => seg.querySelectorAll('button').forEach((b) => b.classList.toggle('active', b.dataset.v === String(get())));
    seg.addEventListener('click', (e) => {
      const b = e.target.closest('button[data-v]');
      if (!b) return;
      set(b.dataset.v);
      savePrefs();
      sync();
      updateButtons();
    });
    sync();
    return sync;
  }
  const syncCountInSeg = wireSeg('audio-countin-seg', () => prefs.countIn, (v) => { prefs.countIn = Number(v); });
  countInBtn?.addEventListener('click', syncCountInSeg);
  const countInEach = $('audio-countin-each');
  if (countInEach) {
    countInEach.checked = !!prefs.countInEach;
    countInEach.addEventListener('change', () => { prefs.countInEach = countInEach.checked; savePrefs(); });
  }
  wireSeg('audio-click-sound-seg', () => prefs.clickSound, (v) => { prefs.clickSound = v; testClick(); });
  const clickVolume = $('audio-click-volume');
  if (clickVolume) {
    clickVolume.value = prefs.clickVolume;
    clickVolume.addEventListener('input', () => { prefs.clickVolume = Number(clickVolume.value); savePrefs(); });
    clickVolume.addEventListener('change', testClick);
  }
  function testClick() {
    if (!clicker) return;
    const ctx = clicker._ensureCtx();
    const beat = 60 / (bpm || 100);
    for (let i = 0; i < beatsPerBar; i++) clickAt(ctx.currentTime + 0.05 + i * beat, i, false);
  }
  $('audio-click-test')?.addEventListener('click', testClick);

  // ---- Speed trainer, as Guitar Pro has it: loop the cycle, starting at
  // one speed and going up by a step every few passes until the target.
  const TRAINER_LIMITS = { start: [25, 150], step: [1, 25], every: [1, 16], end: [25, 150] };
  function trainerSteps() {
    const tr = prefs.trainer;
    const out = [];
    for (let v = tr.start; v < tr.end && out.length < 40; v += tr.step) out.push(v);
    out.push(tr.end);
    return out;
  }
  function renderTrainer() {
    const pop = $('audio-pop-trainer');
    if (!pop) return;
    const tr = prefs.trainer;
    pop.querySelectorAll('.audio-stepper').forEach((st) => {
      const k = st.dataset.key;
      st.querySelector('output').textContent = k === 'every' ? `${tr.every} loop${tr.every > 1 ? 's' : ''}` : k === 'step' ? `+${tr.step}%` : `${tr[k]}%`;
    });
    const on = $('audio-trainer-on');
    if (on) on.checked = !!tr.on;
    const steps = trainerSteps();
    const cur = Math.round(prefs.speed * 100);
    const stairs = $('audio-trainer-stairs');
    if (stairs) {
      stairs.innerHTML = '';
      const lo = Math.min(...steps), hi = Math.max(...steps);
      steps.forEach((v, i) => {
        const bar = document.createElement('div');
        const reached = tr.on && v <= cur;
        bar.className = 'audio-stair' + (reached ? ' reached' : '') + (tr.on && v === cur ? ' now' : '');
        bar.style.height = `${30 + (hi > lo ? ((v - lo) / (hi - lo)) * 70 : 70)}%`;
        bar.title = `${v}%`;
        bar.innerHTML = `<span>${v}</span>`;
        if (steps.length > 12 && i % 2 && i !== steps.length - 1) bar.querySelector('span').textContent = '';
        stairs.appendChild(bar);
      });
    }
    const summary = $('audio-trainer-summary');
    if (summary) {
      const loops = (steps.length - 1) * tr.every;
      const loopSec = cycleRange() ? (cycleRange().t1 - cycleRange().t0) : 0;
      let est = 0;
      steps.slice(0, -1).forEach((v) => { est += (tr.every * loopSec) / (v / 100); });
      const time = loopSec ? ` · about ${Math.max(1, Math.round(est / 60))} min` : '';
      const where = prefs.cycle ? `bars ${barLabel(prefs.cycle.from)}–${barLabel(prefs.cycle.to - 1)}` : 'the section being played';
      summary.textContent = tr.on && passes
        ? `Now ${cur}% · loop ${(passes % tr.every) + 1} of ${tr.every} at this speed${cur < tr.end ? `, then ${Math.min(tr.end, cur + tr.step)}%` : ' — target reached'}.`
        : `${steps.length} speeds, ${loops} loop${loops === 1 ? '' : 's'} to reach ${tr.end}%${time}, on ${where}.`;
    }
  }
  $('audio-pop-trainer')?.addEventListener('click', (e) => {
    const b = e.target.closest('.audio-stepper button[data-d]');
    const preset = e.target.closest('[data-preset]');
    const tr = prefs.trainer;
    if (b) {
      const k = b.closest('.audio-stepper').dataset.key;
      const [lo, hi] = TRAINER_LIMITS[k];
      tr[k] = Math.max(lo, Math.min(hi, tr[k] + Number(b.dataset.d)));
      if (k === 'start' && tr.end <= tr.start) tr.end = Math.min(150, tr.start + 5);
      if (k === 'end' && tr.end <= tr.start) tr.start = Math.max(25, tr.end - 5);
    } else if (preset) {
      const [st, step, every, end] = preset.dataset.preset.split(',').map(Number);
      Object.assign(tr, { start: st, step, every, end });
    } else return;
    if (tr.on) { passes = 0; setSpeed(tr.start / 100, false); }
    savePrefs();
    renderTrainer();
  });
  function setTrainer(on) {
    prefs.trainer.on = on;
    passes = 0;
    if (on) {
      // It works on a loop: make sure there is one.
      if (!prefs.cycle || !prefs.cycle.on) toggleCycle();
      setSpeed(prefs.trainer.start / 100);
    } else {
      setSpeed(1);
    }
    savePrefs();
    updateButtons();
  }
  $('audio-trainer-on')?.addEventListener('change', (e) => setTrainer(e.target.checked));
  $('audio-trainer-start')?.addEventListener('click', () => {
    setTrainer(true);
    pops.forEach((p) => { p.hidden = true; });
    const cyc = cycleRange();
    if (cyc) seek(cyc.t0);
    if (!isPlaying()) play();
  });

  // ---- Ruler: the cycle range, Logic / GarageBand style. Drag across the
  // ruler to set it (snapped to bars), drag its edges to resize it, drag
  // its middle to move it, click it to switch cycling on or off. A
  // section name in the ruler cycles that section; Shift extends. ---------
  const xToTime = (clientX) => {
    const r = waveEl.getBoundingClientRect();
    return (Math.max(0, Math.min(r.width, clientX - r.left)) / r.width) * duration;
  };
  const xToBar = (clientX) => barAtTime(xToTime(clientX));
  const barToX = (b) => {
    const r = waveEl.getBoundingClientRect();
    return r.left + (timeAtBar(b) / duration) * r.width;
  };
  rulerEl?.addEventListener('pointerdown', (e) => {
    if (!hasBars() || !duration || e.button !== 0) return;
    e.preventDefault();
    const x0 = e.clientX;
    const f0 = xToBar(x0);
    const c = prefs.cycle;
    let mode = 'create';
    if (c && cycleEl && !cycleEl.hidden) {
      const l = barToX(c.from), r = barToX(c.to);
      if (Math.abs(x0 - l) <= 7) mode = 'start';
      else if (Math.abs(x0 - r) <= 7) mode = 'end';
      else if (x0 > l && x0 < r) mode = 'move';
    }
    const sectionMark = e.target.closest('.audio-ruler-section');
    const orig = c ? { ...c } : null;
    const last = grid.length - 1;
    let moved = false;
    rulerEl.setPointerCapture(e.pointerId);
    const onMove = (ev) => {
      if (!moved && Math.abs(ev.clientX - x0) < 4) return;
      moved = true;
      const f = xToBar(ev.clientX);
      if (mode === 'create') {
        const a = Math.max(0, Math.floor(Math.min(f0, f)));
        const b = Math.min(last, Math.max(a + 1, Math.ceil(Math.max(f0, f))));
        prefs.cycle = { from: a, to: b, on: true };
      } else if (mode === 'start') {
        prefs.cycle = { ...orig, from: Math.max(0, Math.min(orig.to - 1, Math.round(f))) };
      } else if (mode === 'end') {
        prefs.cycle = { ...orig, to: Math.min(last, Math.max(orig.from + 1, Math.round(f))) };
      } else {
        const len = orig.to - orig.from;
        const from = Math.max(0, Math.min(last - len, orig.from + Math.round(f - f0)));
        prefs.cycle = { ...orig, from, to: from + len };
      }
      renderCycle();
    };
    const onUp = (ev) => {
      rulerEl.removeEventListener('pointermove', onMove);
      rulerEl.removeEventListener('pointerup', onUp);
      if (!moved) {
        if (sectionMark) {
          const start = +sectionMark.dataset.startBar, end = start + (+sectionMark.dataset.barCount);
          prefs.cycle = ev.shiftKey && prefs.cycle
            ? { from: Math.min(prefs.cycle.from, start), to: Math.max(prefs.cycle.to, end), on: true }
            : { from: start, to: end, on: true };
        } else if (mode === 'create') {
          seekToBar(Math.max(0, Math.floor(f0)));
        } else {
          prefs.cycle.on = !prefs.cycle.on;
        }
      }
      passes = 0;
      savePrefs();
      renderCycle();
      updateButtons();
    };
    rulerEl.addEventListener('pointermove', onMove);
    rulerEl.addEventListener('pointerup', onUp);
  });

  // ---- Waveform: hover shows bar · section · time; press and drag to
  // scrub ---------------------------------------------------------------
  function hoverAt(clientX) {
    if (!duration || !hoverEl) return;
    const r = waveEl.getBoundingClientRect();
    const t = xToTime(clientX);
    hoverEl.hidden = false;
    hoverEl.style.left = `${clientX - r.left}px`;
    const pos = barAtTime(t);
    let text = fmtTime(t);
    if (pos !== null && pos >= 0) {
      const b = Math.floor(pos), sec = sectionAt(b);
      text = `Bar ${barLabel(b)}${sec ? ` · ${sec.label}` : ''} · ${text}`;
    }
    hoverLabel.textContent = text;
    hoverEl.classList.toggle('flip', clientX - r.left > r.width - 140);
  }
  waveEl?.addEventListener('pointermove', (e) => hoverAt(e.clientX));
  waveEl?.addEventListener('pointerleave', () => { if (hoverEl) hoverEl.hidden = true; });
  waveEl?.addEventListener('pointerdown', (e) => {
    if (!duration || e.button !== 0) return;
    e.preventDefault();
    waveEl.setPointerCapture(e.pointerId);
    seek(xToTime(e.clientX));
    const onMove = (ev) => { seek(xToTime(ev.clientX)); hoverAt(ev.clientX); };
    const onUp = () => {
      waveEl.removeEventListener('pointermove', onMove);
      waveEl.removeEventListener('pointerup', onUp);
    };
    waveEl.addEventListener('pointermove', onMove);
    waveEl.addEventListener('pointerup', onUp);
  });

  // ---- Keyboard: GarageBand / Logic's, where they have one ---------------
  // Caught before song.js's own page keys (capture phase): with a
  // recording loaded, Space plays it rather than the silent follow-along
  // clock. ←/→ step bars — unless the page has a setlist to step through,
  // where they stay the previous/next song (page-turn pedals send them);
  // "," and "." step bars either way, as in Logic.
  document.addEventListener('keydown', (e) => {
    if (!duration || !playerBody || playerBody.hidden || e.metaKey || e.ctrlKey) return;
    const t = e.target.closest ? e.target : document.body;
    if (t.closest('input, select, textarea, [contenteditable]')) return;
    // A focused button keeps Space and Enter, except the player's own:
    // after clicking Cycle, Space still means play/pause.
    if (t.closest('button, a') && !t.closest('#audio-player')) return;
    const sheet = $('sheet');
    if (sheet && sheet.dataset.renderMode === 'mixer') return; // the Mixer has its own
    const drawer = $('edit-drawer');
    if (drawer && !drawer.hidden) return;
    const songNav = !!document.querySelector('.setlist-nav-card');
    let handled = true;
    switch (e.key) {
      case ' ': case 'Spacebar': isPlaying() ? pause() : play(); break;
      case 'Home': $('audio-start-btn').click(); break;
      case ',': stepBar(-1); break;
      case '.': stepBar(1); break;
      case 'ArrowLeft': if (songNav && !e.altKey) handled = false; else if (e.altKey) stepSection(-1); else stepBar(-1); break;
      case 'ArrowRight': if (songNav && !e.altKey) handled = false; else if (e.altKey) stepSection(1); else stepBar(1); break;
      case 'c': case 'C': case 'l': case 'L': toggleCycle(); break;
      case 'k': case 'K': toggleClick(); break;
      case '[': setSpeed(prefs.speed - (e.shiftKey ? 0.01 : 0.05)); break;
      case ']': setSpeed(prefs.speed + (e.shiftKey ? 0.01 : 0.05)); break;
      default: handled = false;
    }
    if (handled) { e.preventDefault(); e.stopImmediatePropagation(); }
  }, true);

  // ---- Category toggle / load -------------------------------------------
  function selectCategory(cat, remember) {
    activeCategory = cat;
    if (remember) { try { localStorage.setItem(prefKey, cat); } catch (e) {} }
    categoryToggle?.querySelectorAll('button').forEach(b => b.classList.toggle('active', b.dataset.category === cat));
    pause();
    audio.dataset.blobFor = '';
    duration = 0;
    grid = [];
    detected = null;
    decodeWaveform();
  }

  async function loadMeta() {
    const res = await fetch(`/api/songs/${variantId}/audio`);
    if (!res.ok) return;
    const data = await res.json();
    categories = {};
    (data.categories || []).forEach(c => { categories[c.category] = c; });
    const available = Object.keys(categories);

    if (available.length === 0) {
      if (emptyState) emptyState.hidden = false;
      if (playerBody) playerBody.hidden = true;
      if (categoryToggle) categoryToggle.hidden = true;
      return;
    }
    if (emptyState) emptyState.hidden = true;
    if (playerBody) playerBody.hidden = false;

    if (categoryToggle) {
      categoryToggle.hidden = available.length < 2;
      categoryToggle.querySelectorAll('button').forEach(b => { b.hidden = !categories[b.dataset.category]; });
    }

    let preferred = null;
    try { preferred = localStorage.getItem(prefKey); } catch (e) {}
    // Default to "gp" over whatever order the server happened to return
    // (available[0]) when the listener hasn't picked a preference yet —
    // it's the one category guaranteed to line up with the chart's own
    // bars (see store.AudioGP's doc comment), so it's the right silent
    // default even on a song that also has a youtube/band recording.
    const target = (preferred && categories[preferred]) ? preferred : (categories.gp ? 'gp' : available[0]);
    selectCategory(target);
  }

  categoryToggle?.addEventListener('click', e => {
    const btn = e.target.closest('button[data-category]');
    if (btn && categories[btn.dataset.category]) selectCategory(btn.dataset.category, true);
  });

  // Redraw on resize, and when the player itself appears: a page opened
  // on another view (e.g. the Mixer) decodes while the player is 0px wide.
  const redraw = () => { drawWaveform(); refreshTimeline(); };
  window.addEventListener('resize', redraw);
  if (waveEl && window.ResizeObserver) new ResizeObserver(redraw).observe(waveEl);

  // ---- Chart and section strip: jump the recording there -------------------
  // song.js routes a click on a section (strip or chart) here first; a
  // click on a chart bar does the same. True when handled.
  window.songbooksReferenceSeekBar = (bar) => {
    if (!duration || !hasBars()) return false;
    // Asking for a bar outside the cycle means "play from here": switch
    // the cycle off (it stays drawn) rather than snapping back into it.
    const c = prefs.cycle;
    if (c && c.on && (bar < c.from || bar >= c.to)) {
      c.on = false;
      savePrefs();
      renderCycle();
      updateButtons();
    }
    seekToBar(bar);
    if (!isPlaying()) play();
    return true;
  };
  // ---- Chart: click a bar to play from it; press on a bar and drag
  // across others to cycle them (Soundslice / Logic style); Shift-click
  // extends the cycle to that bar. Chord-grid bars and tab rows alike.
  const sheetEl = $('sheet');
  function chartBarAt(e) {
    const el = e.target && e.target.closest ? e.target.closest('.bar[data-global-bar]') : null;
    if (el && el.offsetParent) return Number(el.dataset.globalBar);
    return window.songbooksTabBarAt ? window.songbooksTabBarAt(e.clientX, e.clientY) : null;
  }
  // kind: 'cycle' (the loop, kept on screen) or 'select' (a drag in progress).
  function markChartRange(from, to, kind) {
    document.querySelectorAll(`#sheet .bar.range-${kind}`).forEach((el) => el.classList.remove(`range-${kind}`));
    if (from !== null && to > from) {
      document.querySelectorAll('#sheet .bar[data-global-bar]').forEach((el) => {
        const b = Number(el.dataset.globalBar);
        if (b >= from && b < to) el.classList.add(`range-${kind}`);
      });
    }
    if (window.songbooksTabMarkRange) window.songbooksTabMarkRange(from, to, kind);
  }
  function markCycleInChart() {
    const c = prefs.cycle;
    if (c && c.on && hasBars()) markChartRange(c.from, c.to, 'cycle');
    else markChartRange(null, 0, 'cycle');
  }
  // Tab rows engrave after the page loads (and re-engrave on resize):
  // keep their cycle tint in step.
  setInterval(() => { if (prefs.cycle && prefs.cycle.on && hasBars()) markCycleInChart(); }, 1000);

  function setCycle(from, to) {
    prefs.cycle = { from: Math.max(0, from), to: Math.max(from + 1, to), on: true };
    passes = 0;
    savePrefs();
    renderCycle();
    updateButtons();
    const cyc = cycleRange();
    if (cyc && (audio.currentTime < cyc.t0 - 0.05 || audio.currentTime >= cyc.t1)) seek(cyc.t0);
  }

  let drag = null;
  let suppressClick = false;
  sheetEl?.addEventListener('pointerdown', (e) => {
    if (e.button !== 0 || !hasBars() || $('edit-drawer')?.hidden === false) return;
    const b = chartBarAt(e);
    if (b === null) return;
    drag = { start: b, end: b, x: e.clientX, y: e.clientY, moved: false };
  });
  document.addEventListener('pointermove', (e) => {
    if (!drag) return;
    if (!drag.moved && Math.hypot(e.clientX - drag.x, e.clientY - drag.y) < 6) return;
    drag.moved = true;
    window.getSelection()?.removeAllRanges();
    const b = chartBarAt(e);
    if (b !== null) drag.end = b;
    markChartRange(Math.min(drag.start, drag.end), Math.max(drag.start, drag.end) + 1, 'select');
  });
  document.addEventListener('pointerup', (e) => {
    if (!drag) return;
    const d = drag;
    drag = null;
    markChartRange(null, 0, 'select');
    if (d.moved) {
      setCycle(Math.min(d.start, d.end), Math.max(d.start, d.end) + 1);
    } else if (e.shiftKey) {
      const c = prefs.cycle;
      setCycle(c ? Math.min(c.from, d.start) : d.start, c ? Math.max(c.to, d.start + 1) : d.start + 1);
    } else {
      return; // a plain click: the click handler below plays from that bar
    }
    window.getSelection()?.removeAllRanges();
    suppressClick = true;
    setTimeout(() => { suppressClick = false; }, 0);
  });
  document.addEventListener('pointercancel', () => { drag = null; markChartRange(null, 0, 'select'); });

  function wireBarClickToPlay() {
    if (sheetEl) sheetEl.classList.toggle('bar-click-to-play', hasBars());
    markCycleInChart();
  }
  sheetEl?.addEventListener('click', (e) => {
    if (suppressClick || !hasBars() || e.shiftKey || $('edit-drawer')?.hidden === false) return;
    const b = chartBarAt(e);
    if (b !== null) window.songbooksReferenceSeekBar(b);
  });

  setSpeed(prefs.speed, false);
  updateButtons();

  // ---- Reference-drawer upload controls (YouTube download + band file
  // upload) — moved here from the main player per direct request. Both
  // just call loadMeta() again afterward so the player above picks up
  // the new category without a full page reload. --------------------------
  // The Mixer (mixer.js) plays its own copy of the recording: stop this
  // one when it starts, and reload markers/sync points it edited.
  document.addEventListener('songbooks:pause-reference', () => pause());
  document.addEventListener('songbooks:audio-changed', () => loadMeta());


  if (!canEdit) { loadMeta(); return; }

  function uploadFile(category, file, onDone) {
    const fd = new FormData();
    fd.append('file', file);
    fetch(`/api/songs/${variantId}/audio/${category}`, { method: 'PUT', body: fd })
      .then(async res => {
        if (!res.ok) { alert('Upload failed: ' + (await res.text())); return; }
        loadMeta();
      })
      .finally(() => onDone && onDone());
  }

  // Band recording: plain file upload.
  const bandFileInput = document.getElementById('audio-band-file');
  const bandUploadBtn = document.getElementById('audio-band-upload-btn');
  const bandReplaceBtn = document.getElementById('audio-band-replace-btn');
  const bandDeleteBtn = document.getElementById('audio-band-delete-btn');
  bandUploadBtn?.addEventListener('click', () => bandFileInput?.click());
  bandReplaceBtn?.addEventListener('click', () => bandFileInput?.click());
  bandFileInput?.addEventListener('change', () => {
    const f = bandFileInput.files && bandFileInput.files[0];
    if (f) uploadFile('band', f, () => { bandFileInput.value = ''; });
  });
  bandDeleteBtn?.addEventListener('click', async () => {
    if (!confirm('Remove the band recording?')) return;
    const res = await fetch(`/api/songs/${variantId}/audio/band`, { method: 'DELETE' });
    if (res.ok) loadMeta();
  });

  // YouTube: downloads audio from the video URL already saved as this
  // song's reference (see #reference-block's data-url, kept current by
  // song.js's own initReference) — no second URL field to fill in.
  const ytDownloadBtn = document.getElementById('audio-youtube-download-btn');
  const ytReplaceBtn = document.getElementById('audio-youtube-replace-btn');
  const ytDeleteBtn = document.getElementById('audio-youtube-delete-btn');
  const ytStatus = document.getElementById('audio-youtube-status');
  const referenceBlock = document.getElementById('reference-block');

  async function downloadFromReference() {
    const url = referenceBlock && referenceBlock.dataset.url;
    if (!url) { alert('Add the YouTube video link above first, then download its audio.'); return; }
    ytDownloadBtn && (ytDownloadBtn.disabled = true);
    ytReplaceBtn && (ytReplaceBtn.disabled = true);
    // .btn-loading/.spinner (app.css) — this download can take a real
    // minute (yt-dlp fetch + transcode), and the only in-progress cue
    // before this was the status line's own text, easy to miss below the
    // fold on a long page. A spinner on whichever button was actually
    // clicked makes "it's working" visible right where the person is
    // looking.
    const activeBtn = (ytReplaceBtn && !ytReplaceBtn.hidden) ? ytReplaceBtn : ytDownloadBtn;
    const spinner = document.createElement('span');
    spinner.className = 'spinner';
    activeBtn && activeBtn.classList.add('btn-loading');
    activeBtn && activeBtn.appendChild(spinner);
    if (ytStatus) ytStatus.textContent = 'Downloading… this can take a minute.';
    try {
      const res = await fetch(`/api/songs/${variantId}/audio/youtube`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url }),
      });
      if (!res.ok) {
        if (ytStatus) ytStatus.textContent = 'Download failed: ' + (await res.text());
        return;
      }
      if (ytStatus) ytStatus.textContent = '';
      loadMeta();
    } finally {
      ytDownloadBtn && (ytDownloadBtn.disabled = false);
      ytReplaceBtn && (ytReplaceBtn.disabled = false);
      activeBtn && activeBtn.classList.remove('btn-loading');
      spinner.remove();
    }
  }
  ytDownloadBtn?.addEventListener('click', downloadFromReference);
  ytReplaceBtn?.addEventListener('click', downloadFromReference);
  ytDeleteBtn?.addEventListener('click', async () => {
    if (!confirm('Remove the downloaded YouTube audio?')) return;
    const res = await fetch(`/api/songs/${variantId}/audio/youtube`, { method: 'DELETE' });
    if (res.ok) loadMeta();
  });

  // Reflect which categories already exist in the drawer's own
  // upload/replace/remove button visibility (upload hidden once present,
  // replace/remove shown instead) — re-run whenever loadMeta() refreshes
  // categories, not just once at load.
  // "What's in each recording": full performance or backing track, per
  // category (PUT /api/songs/{id}/audio/{category}/kind).
  const kindList = $('audio-kind-list');
  function renderKinds() {
    if (!kindList) return;
    const cats = Object.keys(categories);
    if (!cats.length) { kindList.innerHTML = '<span class="hint">No recording yet.</span>'; return; }
    const names = { gp: 'Guitar Pro recording', youtube: 'YouTube recording', band: 'Band recording' };
    kindList.innerHTML = '';
    for (const cat of cats) {
      const row = document.createElement('label');
      row.className = 'audio-kind-row';
      row.innerHTML = `<span>${names[cat] || cat}</span><select><option value="">not set</option><option value="full">full performance — every part playing</option><option value="backing">backing track — the part to practise left out</option></select><span class="hint"></span>`;
      const sel = row.querySelector('select');
      const note = row.querySelector('.hint');
      sel.value = categories[cat].kind || '';
      sel.addEventListener('change', async () => {
        const res = await fetch(`/api/songs/${variantId}/audio/${cat}/kind`, {
          method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ kind: sel.value }),
        });
        note.textContent = res.ok ? 'saved' : 'could not save';
        if (res.ok) categories[cat].kind = sel.value;
      });
      kindList.appendChild(row);
    }
  }

  const origLoadMeta = loadMeta;
  loadMeta = async function () {
    await origLoadMeta();
    renderKinds();
    const hasYouTube = !!categories.youtube;
    const hasBand = !!categories.band;
    if (ytDownloadBtn) ytDownloadBtn.hidden = hasYouTube;
    if (ytReplaceBtn) ytReplaceBtn.hidden = !hasYouTube;
    if (ytDeleteBtn) ytDeleteBtn.hidden = !hasYouTube;
    if (bandUploadBtn) bandUploadBtn.hidden = hasBand;
    if (bandReplaceBtn) bandReplaceBtn.hidden = !hasBand;
    if (bandDeleteBtn) bandDeleteBtn.hidden = !hasBand;
  };
  loadMeta();
})();
