// Tap tempo's arithmetic (static/js/tap-tempo.js): the average of the
// last taps, starting over after 2 seconds without one, compared with the
// song's tempo. Run by webassets' TestScripts (go test), or alone, from
// internal/webassets: node --test 'jstest/*.test.mjs'
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const Tap = require('../static/js/tap-tempo.js');

// Taps every `ms` milliseconds from `from`, the last result.
function tapRun(tapper, n, ms, from = 0) {
  let bpm;
  for (let i = 0; i < n; i++) bpm = tapper.tap(from + i * ms);
  return bpm;
}

test('one tap: no tempo yet; two: the beat between them', () => {
  const tp = Tap.create();
  assert.equal(tp.tap(1000), null);
  assert.equal(tp.tap(1500), 120);
});

test('the average of the intervals, not the last one', () => {
  const tp = Tap.create();
  // 500, 600, 400, 500 ms: 2000 ms over 4 beats, 120 bpm.
  for (const at of [0, 500, 1100, 1500]) tp.tap(at);
  assert.equal(tp.tap(2000), 120);
});

test('steady taps give their tempo, rounded', () => {
  assert.equal(tapRun(Tap.create(), 6, 500), 120);
  assert.equal(tapRun(Tap.create(), 5, 600), 100);
  assert.equal(tapRun(Tap.create(), 4, 700), 86); // 85.7
  assert.equal(tapRun(Tap.create(), 8, 250), 240);
});

test('only the last 8 taps count', () => {
  const tp = Tap.create();
  tapRun(tp, 8, 1000); // 60 bpm
  // Then 7 taps at 500 ms: the 8 kept are the last 60 bpm tap and these.
  let bpm;
  for (let i = 1; i <= 7; i++) bpm = tp.tap(7000 + i * 500);
  assert.equal(bpm, 120);
  assert.equal(tp.count(), 8);
});

test('2 seconds without a tap: the next one starts over', () => {
  const tp = Tap.create();
  assert.equal(tapRun(tp, 4, 500), 120); // last tap at 1500
  assert.equal(tp.idle(3400), false);
  assert.equal(tp.idle(3600), true);
  assert.equal(tp.tap(3600), null); // a new run
  assert.equal(tp.count(), 1);
  assert.equal(tp.tap(4350), 80);
});

test('a slow beat under 2 seconds still counts', () => {
  // 1.9 s a beat: about 32 bpm.
  assert.equal(tapRun(Tap.create(), 3, 1900), 32);
});

test('reset forgets the taps', () => {
  const tp = Tap.create();
  tapRun(tp, 3, 500);
  tp.reset();
  assert.equal(tp.count(), 0);
  assert.equal(tp.tap(10000), null);
});

test('bpmOf: nothing for fewer than two taps or no time between them', () => {
  assert.equal(Tap.bpmOf([]), null);
  assert.equal(Tap.bpmOf([5]), null);
  assert.equal(Tap.bpmOf([5, 5]), null);
  assert.equal(Tap.bpmOf([0, 1000]), 60);
});

test('usable: a tempo between 20 and 400 bpm', () => {
  assert.equal(Tap.usable(120), true);
  assert.equal(Tap.usable(20), true);
  assert.equal(Tap.usable(400), true);
  assert.equal(Tap.usable(19), false);
  assert.equal(Tap.usable(401), false);
  assert.equal(Tap.usable(null), false);
});

test("the song's tempo from its header, and the comparison", () => {
  assert.equal(Tap.songTempo('120'), 120);
  assert.equal(Tap.songTempo(' 96 '), 96);
  assert.equal(Tap.songTempo(''), null);
  assert.equal(Tap.songTempo('fast'), null);
  assert.deepEqual(Tap.compare(126, '120'), { song: 120, diff: 6 });
  assert.deepEqual(Tap.compare(114, '120'), { song: 120, diff: -6 });
  assert.deepEqual(Tap.compare(120, '120'), { song: 120, diff: 0 });
  assert.equal(Tap.compare(null, '120'), null);
  assert.equal(Tap.compare(120, ''), null);
});
