// The Song view's rows (static/js/bar-rows.js): 4-bar phrases, a lone
// last bar joining the row before it, a short last row keeping a full
// row's bar width. Run by webassets' TestScripts (go test), or alone,
// from internal/webassets: node --test 'jstest/*.test.mjs'
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const Rows = require('../static/js/bar-rows.js');

// The server's default rows for n bars: 8 to a row, 9 in the first with a
// pickup (groupMeasures, handlers_pages.go).
function serverRows(n, pickup) {
  const out = [];
  let first = pickup ? 9 : 8;
  for (let rest = n + (pickup ? 1 : 0); rest > 0; first = 8) {
    out.push(Math.min(first, rest));
    rest -= first;
  }
  return out;
}
const bars = (plan) => plan.map((r) => r.bars);
const caps = (plan) => plan.map((r) => r.cap);

// Regular bars per row, 1 to 17 bars, 4 to a row.
const FOUR = {
  1: [1], 2: [2], 3: [3], 4: [4], 5: [5], 6: [4, 2], 7: [4, 3], 8: [4, 4],
  9: [4, 5], 10: [4, 4, 2], 11: [4, 4, 3], 12: [4, 4, 4], 13: [4, 4, 5],
  14: [4, 4, 4, 2], 15: [4, 4, 4, 3], 16: [4, 4, 4, 4], 17: [4, 4, 4, 5],
};

test('4 to a row, 1 to 17 bars', () => {
  for (const [n, want] of Object.entries(FOUR)) {
    const p = Rows.plan(serverRows(+n, false), { perRow: 4 });
    assert.deepEqual(bars(p), want, `${n} bars`);
    // A full or a grown row fills the line; a short one keeps 4-bar widths.
    assert.deepEqual(caps(p), want.map((b) => Math.max(b, 4)), `${n} bars: caps`);
  }
});

test('a pickup rides on the first row, the phrases unchanged', () => {
  for (const [n, want] of Object.entries(FOUR)) {
    const p = Rows.plan(serverRows(+n, true), { perRow: 4, pickup: true });
    assert.deepEqual(bars(p), want.map((b, i) => (i === 0 ? b + 1 : b)), `pickup + ${n} bars`);
    assert.deepEqual(caps(p), want.map((b) => Math.max(b, 4)), `pickup + ${n} bars: caps`);
  }
  // The Intro that started this: a count-in, then Gm for 5 bars.
  assert.deepEqual(Rows.plan([6], { perRow: 4, pickup: true }), [{ bars: 6, cap: 5 }]);
  // A section that is only the pickup.
  assert.deepEqual(Rows.plan([1], { perRow: 4, pickup: true }), [{ bars: 1, cap: 1 }]);
});

test('3 and 2 to a row', () => {
  const three = { 1: [1], 3: [3], 4: [4], 5: [3, 2], 6: [3, 3], 7: [3, 4], 10: [3, 3, 4], 11: [3, 3, 3, 2] };
  for (const [n, want] of Object.entries(three)) {
    assert.deepEqual(bars(Rows.plan(serverRows(+n, false), { perRow: 3 })), want, `${n} bars, 3 to a row`);
  }
  const two = { 1: [1], 2: [2], 3: [3], 5: [2, 3], 6: [2, 2, 2], 7: [2, 2, 3] };
  for (const [n, want] of Object.entries(two)) {
    assert.deepEqual(bars(Rows.plan(serverRows(+n, false), { perRow: 2 })), want, `${n} bars, 2 to a row`);
  }
  assert.deepEqual(caps(Rows.plan([5], { perRow: 3 })), [3, 3]);
});

test('a lone last bar joins its row only where the row can hold it', () => {
  // Room for 5 at the legibility floor: one row of 5.
  assert.deepEqual(bars(Rows.plan([5], { perRow: 4, fit: 5 })), [5]);
  // The first row holds less (the clef, the pickup) than the others.
  assert.deepEqual(bars(Rows.plan([5], { perRow: 4, fitFirst: 4, fit: 8 })), [4, 1]);
  assert.deepEqual(bars(Rows.plan([9], { perRow: 4, fitFirst: 4, fit: 5 })), [4, 5]);
  // No room anywhere: a short row of 1, as wide as a bar of the others.
  assert.deepEqual(Rows.plan([9], { perRow: 4, fit: 4 }), [{ bars: 4, cap: 4 }, { bars: 4, cap: 4 }, { bars: 1, cap: 4 }]);
});

test('the server rows are pooled across the whole section', () => {
  // 10 bars come as 8 + 2 from the server, 14 as 8 + 6.
  assert.deepEqual(bars(Rows.plan([8, 2], { perRow: 4 })), [4, 4, 2]);
  assert.deepEqual(bars(Rows.plan([8, 6], { perRow: 4 })), [4, 4, 4, 2]);
  assert.deepEqual(bars(Rows.plan([8, 1], { perRow: 4 })), [4, 5]);
});

test('an explicit bars: line keeps its rows', () => {
  // bars: 3 over 7 bars: 3 + 3 + 1, each bar a third of the line wide.
  assert.deepEqual(Rows.plan([3, 3, 1], { perRow: 4, explicit: true }), [
    { bars: 3, cap: 3 }, { bars: 3, cap: 3 }, { bars: 1, cap: 3 },
  ]);
  // bars: 2: two to a row, as written.
  assert.deepEqual(bars(Rows.plan([2, 2, 2], { perRow: 4, explicit: true })), [2, 2, 2]);
  // bars: 8 is more than a screen row: 4 + 4 per author row.
  assert.deepEqual(bars(Rows.plan([8, 8], { perRow: 4, explicit: true })), [4, 4, 4, 4]);
  // bars: 5 fits as one row of 5; bars: 6 splits as 4 + 2.
  assert.deepEqual(Rows.plan([5, 5], { perRow: 4, explicit: true }), [{ bars: 5, cap: 5 }, { bars: 5, cap: 5 }]);
  assert.deepEqual(Rows.plan([6], { perRow: 4, explicit: true }), [{ bars: 4, cap: 4 }, { bars: 2, cap: 4 }]);
  // bars: 4 with a count-in: the author counts the pickup as one of the 4.
  assert.deepEqual(Rows.plan([4, 4, 4, 4, 1], { perRow: 4, pickup: true, explicit: true }), [
    { bars: 4, cap: 3 }, { bars: 4, cap: 4 }, { bars: 4, cap: 4 }, { bars: 4, cap: 4 }, { bars: 1, cap: 4 },
  ]);
  // bars: 4, 5: a row of 5 stays one row.
  assert.deepEqual(bars(Rows.plan([4, 5, 5], { perRow: 4, explicit: true })), [4, 5, 5]);
});

test('nothing to lay out', () => {
  assert.deepEqual(Rows.plan([], { perRow: 4 }), []);
  assert.deepEqual(Rows.plan([0], { perRow: 4 }), []);
});
