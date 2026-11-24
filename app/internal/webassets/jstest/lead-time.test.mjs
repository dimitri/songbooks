// The song's "lead:" bars in a recording (static/js/lead-time.js): exactly
// N bars ending where the first section starts, nothing counted before.
// Run by webassets' TestScripts (go test), or alone, from
// internal/webassets: node --test 'jstest/*.test.mjs'
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const L = require('../static/js/lead-time.js');
const near = (a, b) => assert.ok(Math.abs(a - b) < 1e-9, `${a} ≉ ${b}`);

// Stand By Me's case: Verse 1 (bar 1) at 0:10.96, 97 bpm in 4/4.
const BAR = (60 / 97) * 4;
const verseAt = (t, lead, bars = 16) => Array.from({ length: lead + bars + 1 }, (_, b) => t + (b - lead) * BAR);

test('one lead bar is one bar, ending where the first section starts', () => {
  const g = L.placeLead(verseAt(10.96, 1), 1, BAR);
  near(g[0], 10.96 - BAR);
  near(g[1], 10.96);
  // Nothing before it: bar 0 is the first bar the grid has.
  assert.equal(L.firstCounted(-3.4), 0);
});

test('a grid that ran the lead back from 0:00 is put right', () => {
  // A detection that spread the lead over the whole pre-roll (bar 0 at
  // 0:00): one bar long again, the 8.5 s before it counted as nothing.
  const g = verseAt(10.96, 1);
  g[0] = 0;
  const out = L.placeLead(g, 1, L.firstPace(g, 1, 16));
  near(out[0], 10.96 - BAR);
});

test('the lead bars take the first section pace', () => {
  const g = verseAt(20, 2).map((t, b) => (b >= 2 ? 20 + (b - 2) * 2 : t)); // played at 2 s a bar
  near(L.firstPace(g, 2, 16), 2);
  const out = L.placeLead(g, 2, L.firstPace(g, 2, 16));
  assert.deepEqual(out.slice(0, 3), [16, 18, 20]);
});

test('a lead longer than the time before the first section is squeezed into it', () => {
  const out = L.placeLead(verseAt(1.2, 2), 2, BAR);
  near(out[0], 0);
  near(out[1], 0.6);
  near(out[2], 1.2);
});

test('with the first section at 0:00 the lead stays before the recording', () => {
  const out = L.placeLead(verseAt(0, 1), 1, BAR);
  near(out[0], -BAR);
});

test('no lead, no change', () => {
  const g = verseAt(5, 0);
  assert.equal(L.placeLead(g, 0, BAR), g);
});
