// node --test: the Sync sheet's counter tempo (sync-wave-math.js
// rowTempos, medianTempo, tempoFix, barLength): the tempo the chart's bars
// make between the sections' starts, in the chart's beat, against the
// chart's tempo; the warning's one-click fix; ±1 bar on the last start.
const test = require('node:test');
const assert = require('node:assert/strict');
const M = require('../static/js/sync-wave-math.js');

// Rows of `bars` bars each, the starts `secs` seconds apart.
const even = (n, bars, secs, from = 0) => Array.from({ length: n }, (_, i) => ({ startBar: i * bars, t: from + i * secs }));

test('4/4: 8 bars in 16 s is 120 bpm', () => {
  const rows = even(3, 8, 16);
  assert.deepEqual(M.rowTempos(rows, 4), [120, 120, 0]);
  assert.equal(M.medianTempo(rows, 4), 120);
  assert.equal(M.tempoFix(rows, 4, 120), 0); // on tempo: nothing to fix
});

test('3/4: 8 bars in 12 s is 120 bpm', () => {
  assert.equal(M.medianTempo(even(3, 8, 12), 3), 120);
});

test('6/8 counts dotted quarters: Wake Up Alone, 16 bars in 42.08 s', () => {
  // Intro 0:00.55, Verse 1 0:42.63, Verse 2 1:24.71, Chorus 1 2:06.79.
  const rows = [{ startBar: 0, t: 0.55 }, { startBar: 16, t: 42.63 }, { startBar: 32, t: 84.71 }, { startBar: 48, t: 126.79 }, { startBar: 64, t: null }];
  const per = M.rowTempos(rows, 2);
  assert.equal(Math.round(per[0]), 46);
  assert.equal(per[3], 0);
  assert.equal(per[4], 0);
  // Against the chart's 72: off, and the fix is the counter's 46…
  assert.equal(M.offTempo(per[0], 72), true);
  assert.equal(M.tempoFix(rows, 2, 72), 46);
  // …which, taken, leaves nothing off and nothing more to fix.
  assert.equal(M.offTempo(per[0], 46), false);
  assert.equal(M.tempoFix(rows, 2, 46), 0);
});

test('12/8 counts dotted quarters: 4 bars of 4 at 60 bpm is 16 s', () => {
  const rows = even(2, 4, 16);
  assert.equal(M.medianTempo(rows, 4), 60);
  assert.equal(M.tempoFix(rows, 4, 90), 60);
});

test('the median is weighted by bars: one short odd section does not win', () => {
  // 32 bars at 100, then 4 bars at 140 (a section with bars too many).
  const rows = [{ startBar: 0, t: 0 }, { startBar: 32, t: 76.8 }, { startBar: 36, t: 76.8 + 6.857 }, { startBar: 40, t: null }];
  assert.equal(Math.round(M.medianTempo(rows, 4)), 100);
});

test('no chart tempo: the counter\'s is proposed; one start alone: nothing', () => {
  assert.equal(M.tempoFix(even(2, 8, 16), 4, 0), 120);
  assert.equal(M.tempoFix([{ startBar: 0, t: 1 }, { startBar: 8, t: null }], 4, 0), 0);
});

test('±1 bar: the span when known, the one before on the last start, else the chart', () => {
  const rows = [{ startBar: 0, t: 0.55 }, { startBar: 16, t: 42.63 }, { startBar: 32, t: null }];
  const bar = 42.08 / 16;
  assert.ok(Math.abs(M.barLength(rows, 0, 1.667) - bar) < 1e-9);
  // The last start placed: the recording's pace before it, not 72's 1.67 s.
  assert.ok(Math.abs(M.barLength(rows, 1, 1.667) - bar) < 1e-9);
  // Not placed: the chart's.
  assert.equal(M.barLength(rows, 2, 1.667), 1.667);
  assert.equal(M.barLength([{ startBar: 0, t: 3 }], 0, 2.5), 2.5);
});
