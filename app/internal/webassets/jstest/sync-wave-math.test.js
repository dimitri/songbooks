// node --test: the Sync sheet's waveform arithmetic (static/js/sync-wave-math.js).
// Run by jstest_test.go (go test ./internal/webassets) when node is installed.
const test = require('node:test');
const assert = require('node:assert/strict');
const M = require('../static/js/sync-wave-math.js');

const near = (a, b, eps = 1e-9) => assert.ok(Math.abs(a - b) <= eps, `${a} ≉ ${b}`);

test('clampView keeps the window inside the recording, at least MIN_SPAN wide', () => {
  assert.deepEqual(M.clampView({ start: -5, span: 10 }, 100), { start: 0, span: 10 });
  assert.deepEqual(M.clampView({ start: 95, span: 10 }, 100), { start: 90, span: 10 });
  assert.deepEqual(M.clampView({ start: 10, span: 500 }, 100), { start: 0, span: 100 });
  assert.equal(M.clampView({ start: 10, span: 0.1 }, 100).span, M.MIN_SPAN);
  // Shorter than MIN_SPAN: the whole of it.
  assert.deepEqual(M.clampView({ start: 0, span: 0.2 }, 1), { start: 0, span: 1.5 });
});

test('tToX and xToT are inverse', () => {
  const v = { start: 12, span: 8 };
  for (const t of [12, 13.5, 20]) near(M.xToT(M.tToX(t, v, 800), v, 800), t);
  near(M.tToX(16, v, 800), 400);
});

test('zoomAt keeps the anchor time under the same pixel', () => {
  const v = { start: 20, span: 40 };
  const at = 30; // a quarter in
  const z = M.zoomAt(v, 4, at, 200);
  near(z.span, 10);
  near(M.tToX(at, z, 1000), M.tToX(at, v, 1000));
  // Out past the whole song: the whole song.
  assert.deepEqual(M.zoomAt(v, 0.01, at, 200), { start: 0, span: 200 });
  // In past a few beats: MIN_SPAN.
  near(M.zoomAt(v, 1000, at, 200).span, M.MIN_SPAN);
});

test('pan scrolls by pixels and stops at the ends', () => {
  const v = { start: 10, span: 20 };
  near(M.pan(v, 100, 1000, 100).start, 12);
  near(M.pan(v, -10000, 1000, 100).start, 0);
  near(M.pan(v, 100000, 1000, 100).start, 80);
});

test('pinch: spreading the fingers zooms in around their midpoint', () => {
  // At the start: midpoint over t = 50, span 40, 100 px apart; then 200 px
  // apart with the midpoint at x = 500 of 1000.
  const v = M.pinch(50, 40, 100, 200, 500, 1000, 200);
  near(v.span, 20);
  near(M.xToT(500, v, 1000), 50);
});

test('follow: the playhead runs to the middle, then the waveform scrolls', () => {
  const v = { start: 0, span: 20 };
  assert.equal(M.follow(v, 5, 100), v);
  near(M.follow(v, 15, 100).start, 5);
  // A seek out of the window: back in, a little from the left.
  near(M.follow(v, 60, 100).start, 58);
  near(M.follow({ start: 50, span: 20 }, 10, 100).start, 8);
  // Near the end it stops scrolling.
  near(M.follow({ start: 70, span: 20 }, 95, 100).start, 80);
});

test('reveal brings an off-screen time a quarter in, leaves a visible one', () => {
  const v = { start: 0, span: 20 };
  assert.equal(M.reveal(v, 10, 100), v);
  near(M.reveal(v, 50, 100).start, 45);
});

test('buildPeaks: levels halve, each the peak of two below, normalised to the loudest', () => {
  const sr = 1000;
  const x = new Float32Array(sr * 60); // a minute
  x[12345] = -0.5;
  x[40000] = 0.25;
  const p = M.buildPeaks(x, sr, 32);
  assert.equal(p.levels[0].length, Math.ceil(x.length / 32));
  for (let k = 1; k < p.levels.length; k++) assert.equal(p.levels[k].length, Math.ceil(p.levels[k - 1].length / 2));
  assert.ok(p.levels[p.levels.length - 1].length <= 256);
  near(p.top, 0.5);
  near(p.levels[0][Math.floor(12345 / 32)], 0.5);
  near(p.levels[3][Math.floor(40000 / 256)], 0.25);
});

test('columns: one peak per pixel at any zoom, from the right level', () => {
  const sr = 1000;
  const x = new Float32Array(sr * 100);
  x[50000] = 1; // a click at 50 s
  const p = M.buildPeaks(x, sr, 32);
  // The whole song on 1000 px: 100 samples a pixel, level 1 (64-sample buckets).
  assert.equal(M.levelFor(p, 100), 1);
  const all = M.columns(p, { start: 0, span: 100 }, 1000);
  assert.equal(all.length, 1000);
  assert.equal(all.reduce((n, a) => n + (a > 0 ? 1 : 0), 0), 1);
  near(all[500], 1);
  // Two seconds on 1000 px: 2 samples a pixel, level 0; the click at 50 % of [49, 51].
  const close = M.columns(p, { start: 49, span: 2 }, 1000);
  const lit = [...close.keys()].filter((i) => close[i] > 0);
  assert.ok(lit.length >= 1 && lit.every((i) => Math.abs(i - 500) <= 16), JSON.stringify(lit));
  // Off the end: silence, no error.
  assert.equal(M.columns(p, { start: 99, span: 5 }, 100).length, 100);
});

test('barTimes spreads each section evenly up to the next start, the tempo outside', () => {
  // Sections at bars 0 (t = 2), 4 (t = 10), 12 (t = 30); 2 s a bar.
  const bars = M.barTimes([{ start: 0, t: 2 }, { start: 4, t: 10 }, { start: 12, t: 30 }], 2, 40);
  near(bars[0], 2);
  near(bars[2], 6);
  near(bars[4], 10);
  near(bars[8], 20); // (30 − 10) / 8 = 2.5 s a bar
  near(bars[12], 30);
  near(bars[13], 32); // after the last: the chart's tempo
  assert.ok(bars[bars.length - 1] > 40 && bars[bars.length - 2] <= 40);
  // No tempo: the pace between the first and last starts.
  const b2 = M.barTimes([{ start: 2, t: 4 }, { start: 6, t: 12 }], 0, 30);
  near(b2[0], 0);
  near(b2[7], 14);
  // Unplaced sections don't count; none placed, no bars.
  assert.deepEqual(M.barTimes([{ start: 0, t: null }], 2, 10), []);
});

test('timeAt and barAt are inverse along the bars', () => {
  const bars = [1, 3, 5, 8, 11];
  near(M.timeAt(bars, 2.5), 6.5);
  near(M.barAt(bars, 6.5), 2.5);
  near(M.barAt(bars, 0), -0.5); // before bar 0: its pace
  near(M.timeAt(bars, 5), 14); // past the end: the last bar's pace
});

test('labelEvery numbers bars so labels keep apart', () => {
  assert.equal(M.labelEvery(40), 1);
  assert.equal(M.labelEvery(20), 2);
  assert.equal(M.labelEvery(5), 8);
  assert.equal(M.labelEvery(0.1), 128);
});

test('snap: the nearest target within its reach, downbeats reaching furthest', () => {
  const targets = { downbeat: [10, 20], beat: [10.5, 11, 19.5], voice: [12.02], suggestion: [] };
  const pps = 100; // 100 px a second: a downbeat reaches 0.12 s, a beat 0.07 s
  assert.deepEqual(M.snap(10.1, targets, pps), { t: 10, kind: 'downbeat' });
  assert.deepEqual(M.snap(10.47, targets, pps), { t: 10.5, kind: 'beat' });
  assert.deepEqual(M.snap(12.0, targets, pps), { t: 12.02, kind: 'voice' });
  assert.deepEqual(M.snap(15, targets, pps), { t: 15, kind: null });
  // Between a beat 0.04 s away and a downbeat 0.06 s away: the downbeat
  // (closer for its reach).
  assert.deepEqual(M.snap(19.94, targets, pps), { t: 20, kind: 'downbeat' });
  // Zoomed out, the reach in seconds grows.
  assert.deepEqual(M.snap(10.8, { downbeat: [10] }, 10), { t: 10, kind: 'downbeat' });
});

test('between keeps a start between its neighbours', () => {
  near(M.between(5, 6, 10, 100), 6.2);
  near(M.between(11, 6, 10, 100), 9.8);
  near(M.between(-3, null, null, 100), 0);
  near(M.between(7, null, null, 100), 7);
});
