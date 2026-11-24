// node --test: taking the Sync sheet's suggestions (sync-wave-math.js acceptPicks).
const test = require('node:test');
const assert = require('node:assert/strict');
const M = require('../static/js/sync-wave-math.js');

const rows = () => [
  { t: 10, origin: 'auto' }, // Intro
  { t: 60, origin: 'set' }, // Verse 2, by ear
  { t: 75, origin: 'set' }, // Chorus, by ear
  { t: 90, origin: 'auto' }, // Outro
];

test('one suggestion past the next start is refused, nothing moves', () => {
  // The voice says Verse 2 starts at 80: after the Chorus.
  const r = M.acceptPicks(rows(), { 1: 80 }, { strict: true });
  assert.deepEqual(r.refused, [1]);
  assert.deepEqual(r.rows.map((x) => x.t), [10, 60, 75, 90]);
});

test('one suggestion between its neighbours is taken, as set', () => {
  const r = M.acceptPicks(rows(), { 1: 62 }, { strict: true });
  assert.deepEqual(r.refused, []);
  assert.equal(r.rows[1].t, 62);
  assert.equal(r.rows[1].origin, 'set');
});

test('strict: an estimate after it counts as a neighbour too', () => {
  const r = M.acceptPicks(rows(), { 2: 95 }, { strict: true });
  assert.deepEqual(r.refused, [2]);
});

test('accept all never drops a start set by ear', () => {
  // The Intro's suggestion lands after Verse 2 (set by ear), the Outro's is fine.
  const r = M.acceptPicks(rows(), { 0: 65, 3: 88 });
  assert.deepEqual(r.refused, [0]);
  assert.deepEqual(r.rows.map((x) => x.t), [10, 60, 75, 88]);
  assert.equal(r.rows[1].origin, 'set');
});

test('accept all: picks are checked against the picks before them', () => {
  const r = M.acceptPicks([{ t: null, origin: null }, { t: null, origin: null }, { t: 50, origin: 'auto' }], { 0: 20, 1: 15 });
  assert.deepEqual(r.refused, [1]);
  assert.equal(r.rows[0].t, 20);
  assert.equal(r.rows[1].t, null);
  assert.equal(r.rows[2].t, 50);
});

test('an estimate the picks leave behind is dropped', () => {
  const r = M.acceptPicks([{ t: 10, origin: 'auto' }, { t: 30, origin: 'auto' }, { t: 100, origin: 'set' }], { 0: 40 });
  assert.deepEqual(r.refused, []);
  assert.equal(r.rows[0].t, 40);
  assert.equal(r.rows[1].t, null);
  assert.equal(r.rows[2].t, 100);
});
