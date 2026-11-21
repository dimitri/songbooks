// The Sync sheet's waveform (sync-wave.js), its arithmetic: the visible
// window and how zooming, scrolling and following the playhead move it,
// the peaks at several resolutions, where the bars fall between the
// sections' starts, and where a dragged start snaps. Pure: no DOM, so
// node tests it (app/internal/webassets/jstest/sync-wave-math.test.js).
//
// A view is {start, span} in seconds: the window [start, start + span)
// drawn across the canvas' width.
(function (root, factory) {
  if (typeof module === 'object' && module.exports) module.exports = factory();
  else root.SyncWaveMath = factory();
})(typeof self !== 'undefined' ? self : this, function () {
  // The closest a view gets: a few beats at any tempo.
  const MIN_SPAN = 1.5;

  function clampView(v, dur, minSpan = MIN_SPAN) {
    const max = Math.max(minSpan, dur || 0);
    const span = Math.max(Math.min(minSpan, max), Math.min(max, v.span));
    const start = Math.max(0, Math.min(Math.max(0, (dur || 0) - span), v.start));
    return { start, span };
  }
  const fit = (dur) => ({ start: 0, span: Math.max(MIN_SPAN, dur || 0) });
  const tToX = (t, v, w) => ((t - v.start) / v.span) * w;
  const xToT = (x, v, w) => v.start + (x / w) * v.span;

  // Zoom by factor (> 1: in) keeping time `at` where it is on screen.
  function zoomAt(v, factor, at, dur) {
    const frac = v.span > 0 ? (at - v.start) / v.span : 0.5;
    const span = clampView({ start: 0, span: v.span / factor }, dur).span;
    return clampView({ start: at - frac * span, span }, dur);
  }
  // Scroll by px pixels of a w pixels wide canvas.
  const pan = (v, px, w, dur) => clampView({ start: v.start + (px / w) * v.span, span: v.span }, dur);
  // Pinch: the time under the fingers' midpoint at the start (at), the
  // span then (span0) and the fingers' distances then and now; the
  // midpoint is now at x.
  function pinch(at, span0, d0, d1, x, w, dur) {
    const span = clampView({ start: 0, span: span0 * (d0 / Math.max(1, d1)) }, dur).span;
    return clampView({ start: at - (x / w) * span, span }, dur);
  }
  // Following the playhead: it moves across until the middle, then the
  // waveform scrolls under it; a seek out of the window brings it back,
  // a little in from the left edge.
  function follow(v, t, dur) {
    if (t < v.start || t > v.start + v.span) return clampView({ start: t - v.span * 0.1, span: v.span }, dur);
    if (t > v.start + v.span * 0.5) return clampView({ start: t - v.span * 0.5, span: v.span }, dur);
    return v;
  }
  // Show time t (a section's start) when it's off screen, a quarter in.
  function reveal(v, t, dur) {
    if (t >= v.start + v.span * 0.04 && t <= v.start + v.span * 0.96) return v;
    return clampView({ start: t - v.span * 0.25, span: v.span }, dur);
  }

  // ---- Peaks -------------------------------------------------------------
  // x: mono samples at sr. Level 0 holds the peak of every `base` samples,
  // each level above it the peak of two of the level below.
  function buildPeaks(x, sr, base = 32) {
    const n0 = Math.ceil(x.length / base);
    const l0 = new Float32Array(n0);
    for (let i = 0, j = 0; i < n0; i++) {
      let m = 0;
      const end = Math.min(x.length, j + base);
      for (; j < end; j++) { const a = x[j] < 0 ? -x[j] : x[j]; if (a > m) m = a; }
      l0[i] = m;
    }
    const levels = [l0];
    while (levels[levels.length - 1].length > 256) {
      const p = levels[levels.length - 1];
      const q = new Float32Array(Math.ceil(p.length / 2));
      for (let i = 0; i < q.length; i++) q[i] = Math.max(p[2 * i], p[2 * i + 1] || 0);
      levels.push(q);
    }
    // Loudness drawn against the loudest moment, not full scale.
    let top = 0;
    for (const a of levels[levels.length - 1]) if (a > top) top = a;
    return { sr, base, levels, top: top || 1 };
  }
  // The level for a view: the coarsest whose buckets still fit one in
  // each pixel column.
  function levelFor(peaks, samplesPerPx) {
    let k = 0;
    while (k + 1 < peaks.levels.length && peaks.base * 2 ** (k + 1) <= samplesPerPx) k++;
    return k;
  }
  // One peak per pixel column of the view, 0..1.
  function columns(peaks, v, w) {
    const out = new Float32Array(Math.max(0, Math.floor(w)));
    if (!peaks || !out.length) return out;
    const spp = (v.span * peaks.sr) / w;
    const k = levelFor(peaks, spp);
    const lv = peaks.levels[k];
    const bucket = peaks.base * 2 ** k; // samples
    const perPx = spp / bucket; // buckets per column
    const first = (v.start * peaks.sr) / bucket;
    for (let i = 0; i < out.length; i++) {
      const a = Math.floor(first + i * perPx);
      const b = Math.max(a + 1, Math.floor(first + (i + 1) * perPx));
      let m = 0;
      for (let j = Math.max(0, a); j < b && j < lv.length; j++) if (lv[j] > m) m = lv[j];
      out[i] = Math.min(1, m / peaks.top);
    }
    return out;
  }

  // ---- The bars -----------------------------------------------------------
  // When each bar starts, from the sections' starts (marks: [{start (the
  // section's first bar), t}]): a section's bars spread evenly up to the
  // next start; before the first and after the last, the chart's tempo
  // (secondsPerBar), else the pace between the first and the last.
  // audio-player.js' rebuildGrid does the same for the chart.
  function barTimes(marks, secondsPerBar, dur, maxBars = 5000) {
    const m = marks.filter((x) => x && x.t !== null && x.t !== undefined && isFinite(x.t)).slice().sort((a, b) => a.start - b.start);
    if (!m.length) return [];
    let pace = secondsPerBar;
    if (!pace && m.length > 1 && m[m.length - 1].start > m[0].start) pace = (m[m.length - 1].t - m[0].t) / (m[m.length - 1].start - m[0].start);
    if (!pace) return [];
    const out = [];
    let i = 0;
    for (let b = 0; b < maxBars; b++) {
      while (i < m.length - 1 && m[i + 1].start <= b) i++;
      const a = m[i], next = m[i + 1];
      const t = b >= a.start && next && next.start > a.start ? a.t + ((b - a.start) / (next.start - a.start)) * (next.t - a.t) : a.t + (b - a.start) * pace;
      out.push(t);
      if (t > dur) break;
    }
    return out;
  }
  // Time at fractional bar b on bars (times), and back.
  function timeAt(bars, b) {
    if (bars.length < 2) return null;
    const i = Math.max(0, Math.min(bars.length - 2, Math.floor(b)));
    return bars[i] + (b - i) * (bars[i + 1] - bars[i]);
  }
  function barAt(bars, t) {
    if (bars.length < 2) return null;
    let lo = 0, hi = bars.length - 1;
    if (t < bars[0]) hi = 1;
    else if (t >= bars[hi]) lo = hi - 1;
    else while (hi - lo > 1) { const mid = (lo + hi) >> 1; if (bars[mid] <= t) lo = mid; else hi = mid; }
    return lo + (t - bars[lo]) / (bars[lo + 1] - bars[lo] || 1);
  }
  // Number every k-th bar so numbers keep minPx apart.
  const labelEvery = (barPx, minPx = 28) => [1, 2, 4, 8, 16, 32, 64].find((k) => barPx * k >= minPx) || 128;

  // ---- Snapping ----------------------------------------------------------------
  // How far each kind of target reaches, in pixels: a dragged start lands
  // on a detected downbeat sooner than on a voice onset, a beat last.
  const REACH = { suggestion: 12, downbeat: 12, voice: 9, beat: 7 };
  // targets: {downbeat: [t], beat: [t], voice: [t], suggestion: [t]}.
  // The nearest within its reach, closest relative to that reach; else t.
  function snap(t, targets, pxPerSec) {
    let best = null, score = Infinity;
    for (const kind of Object.keys(REACH)) {
      const list = targets[kind];
      if (!list || !list.length) continue;
      const reach = REACH[kind] / pxPerSec;
      // Sorted lists: a binary search for the neighbours.
      let lo = 0, hi = list.length;
      while (lo < hi) { const mid = (lo + hi) >> 1; if (list[mid] < t) lo = mid + 1; else hi = mid; }
      for (const j of [lo - 1, lo]) {
        if (j < 0 || j >= list.length) continue;
        const d = Math.abs(list[j] - t);
        if (d <= reach && d / reach < score) { score = d / reach; best = { t: list[j], kind }; }
      }
    }
    return best || { t, kind: null };
  }
  // A section start stays between its neighbours (gap seconds from each).
  function between(t, prev, next, dur, gap = 0.2) {
    let x = t;
    if (prev !== null && prev !== undefined) x = Math.max(prev + gap, x);
    if (next !== null && next !== undefined) x = Math.min(next - gap, x);
    return Math.max(0, Math.min(dur || Infinity, x));
  }

  return { MIN_SPAN, clampView, fit, tToX, xToT, zoomAt, pan, pinch, follow, reveal, buildPeaks, levelFor, columns, barTimes, timeAt, barAt, labelEvery, snap, between, REACH };
});
