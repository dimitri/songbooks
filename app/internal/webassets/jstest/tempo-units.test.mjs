// What a chart's tempo counts (static/js/tempo-units.js, as
// internal/song/tempo.go): the metre's beat, a dotted quarter in 6/8,
// 9/8, 12/8; said unambiguously and typed in the note picked. Run by
// webassets' TestScripts (go test), or alone: node --test 'jstest/*.test.mjs'
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const TU = require('../static/js/tempo-units.js');

test('compound metres count dotted quarters, others their denominator', () => {
  for (const s of ['6/8', '9/8', '12/8', '15/8']) assert.equal(TU.compound(s), true, s);
  for (const s of ['4/4', '3/4', '2/2', '3/8', '7/8', '6/4', '', 'x']) assert.equal(TU.compound(s), false, s);
  assert.equal(TU.compound(6, 8), true);
  assert.equal(TU.compound('12', '8'), true);
  assert.equal(TU.beatQuarters(6, 8), 1.5);
  assert.equal(TU.beatQuarters(12, 8), 1.5);
  assert.equal(TU.beatQuarters(4, 4), 1);
  assert.equal(TU.beatQuarters(3, 4), 1);
  assert.equal(TU.beatQuarters(2, 2), 2);
  assert.equal(TU.beatQuarters(7, 8), 0.5);
  assert.equal(TU.beatQuarters('junk'), 1);
});

test('a bpm is said in its note in a compound metre only', () => {
  assert.equal(TU.text(45.6, 6, 8), '♩. = 46 (♪ = 137)');
  assert.equal(TU.text(72, '6/8'), '♩. = 72 (♪ = 216)');
  assert.equal(TU.text(46, 12, 8, { short: true }), '♩. = 46');
  assert.equal(TU.text(120, 4, 4), '120 bpm');
  assert.equal(TU.text(89.6, 3, 4), '90 bpm');
});

test('a tempo typed in eighths or quarters is kept in dotted quarters', () => {
  // 6/8: 137 eighths, 69 quarters and 46 dotted quarters are one tempo.
  assert.equal(Math.round(TU.toBeat(137, 'eighth', 6, 8)), 46);
  assert.equal(TU.toBeat(69, 'quarter', 6, 8), 46);
  assert.equal(TU.toBeat(46, 'beat', 6, 8), 46);
  assert.equal(TU.fromBeat(46, 'eighth', 6, 8), 138);
  assert.equal(TU.fromBeat(46, 'quarter', 6, 8), 69);
  // A simple metre has one obvious beat: nothing to convert, no picker.
  assert.equal(TU.toBeat(120, 'eighth', 4, 4), 120);
  assert.equal(TU.units(4, 4), null);
  assert.deepEqual(TU.units(12, 8).map((u) => u.id), ['beat', 'quarter', 'eighth']);
});
