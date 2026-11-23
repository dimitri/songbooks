// The chords over a Guitar Pro file's bars (static/js/gp-chords.js): the
// chart's chords put on the file's bars through the song's bar mapping
// (gp[...] lines, gp_start, a "lead:"), repeats included, and placed on
// alphaTab's beats. Run by webassets' TestScripts (go test), or alone,
// from internal/webassets: node --test 'jstest/*.test.mjs'
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const C = require('../static/js/gp-chords.js');
const A = require('../static/js/gp-align.js');
const GPFacts = require('../static/js/gp-facts.js');
const alphaTab = require('../static/js/vendor/alphatab/alphaTab.min.js');

const ch = (...names) => names.map((n, i) => ({ pos: i / names.length, name: n }));
// Chart chords by chart bar, as the Mixer's sections give them.
const chart = (...bars) => C.byChartBar([{ startBar: 0, chords: bars.map((b) => (b ? ch(...b.split(' ')) : [])) }]);
// The chords for each written bar (0-based), as plain names.
function written(order, played, chords) {
  const m = C.forWrittenBars(A.chartBarsForFile(order, played), played, chords);
  return Object.fromEntries([...m].map(([w, l]) => [w, l.map((c) => c.name).join(' ')]));
}

test('the chart’s sections, by chart bar', () => {
  const by = C.byChartBar([
    { startBar: 0, chords: [[{ pos: 0, name: 'E7' }], [], [{ pos: 0, name: 'A7' }, { pos: 0.5, name: '' }]] },
    { startBar: 3, chords: [[{ pos: 0.5, name: 'B7' }]] },
  ]);
  assert.deepEqual(by[0], [{ pos: 0, name: 'E7' }]);
  assert.equal(by[1], undefined);
  assert.deepEqual(by[2], [{ pos: 0, name: 'A7' }]);
  assert.deepEqual(by[3], [{ pos: 0.5, name: 'B7' }]);
});

test('bar for bar (gp[...] lines, or gp_start 1)', () => {
  assert.deepEqual(written([1, 2, 3], [1, 2, 3], chart('E7', 'A7', 'B7 A7')), { 0: 'E7', 1: 'A7', 2: 'B7 A7' });
});

test('a repeat: the written bar shows its first pass', () => {
  // |: 1 2 :| 3 — played 1 2 1 2 3; the chart writes it out.
  const masterBars = [{ isRepeatStart: true }, { repeatCount: 2 }, {}];
  const played = GPFacts.played(masterBars);
  assert.deepEqual(played, [1, 2, 1, 2, 3]);
  assert.deepEqual(written([1, 2, 1, 2, 3], played, chart('C', 'G', 'Am', 'F', 'E')), { 0: 'C', 1: 'G', 2: 'E' });
});

test('gp_start: the file’s lead-in bars get none', () => {
  // gp_start: 3 — the chart's bar 1 is the file's bar 3.
  assert.deepEqual(written([3, 4, 5], [1, 2, 3, 4, 5], chart('D', 'G', 'A')), { 2: 'D', 3: 'G', 4: 'A' });
});

test('a "lead:" the file doesn’t play: its chords aren’t put anywhere', () => {
  // lead: | G | — two bars before the file starts (gpOrder 0).
  assert.deepEqual(written([0, 0, 1, 2], [1, 2], chart('G', 'G', 'E7', 'A7')), { 0: 'E7', 1: 'A7' });
});

test('no chart chords, no mapping: nothing', () => {
  assert.equal(C.forWrittenBars(null, [1, 2], chart('E7', 'A7')).size, 0);
  assert.equal(C.forWrittenBars([0, 1], [1, 2], []).size, 0);
});

// A real alphaTab score: four quarter notes, two halves, a whole note.
function score() {
  const imp = new alphaTab.importer.AlphaTexImporter();
  imp.initFromString('\\title "T"\n\\track "Guitare"\n.\n:4 0.6 2.5 2.4 0.3 | :2 0.6 0.5 | :1 0.6 |', new alphaTab.Settings());
  return imp.readScore();
}
const makeChord = (name) => { const c = new alphaTab.model.Chord(); c.name = name; c.showDiagram = false; return c; };
const names = (sc) => sc.tracks[0].staves[0].bars.map((b) => b.voices[0].beats.map((x) => (x.hasChord ? x.chord.name : '')).join('|'));

test('put on alphaTab’s beats, where in the bar they fall', () => {
  const sc = score();
  assert.equal(C.hasOwn(sc), false);
  const chords = new Map([[0, ch('E7', 'A7')], [1, [{ pos: 0.75, name: 'B7' }]], [2, ch('E7', 'B7')]]);
  assert.equal(C.put(sc, 0, chords, makeChord), 4);
  // E7 on beat 1, A7 on beat 3; B7 at ¾ on the second half note; a whole
  // note under two chords carries both names.
  assert.deepEqual(names(sc), ['E7||A7|', '|B7', 'E7 · B7']);
  // Ours, not the file's: again, they replace the last ones.
  assert.equal(C.hasOwn(sc), false);
  C.put(sc, 0, new Map([[1, ch('D')]]), makeChord);
  assert.deepEqual(names(sc), ['|||', 'D|', '']);
  C.clear(sc);
  assert.deepEqual(names(sc), ['|||', '|', '']);
});

test('the file’s own chord symbols are kept, and win', () => {
  const sc = score();
  const st = sc.tracks[0].staves[0];
  st.addChord('1', makeChord('Em'));
  st.bars[0].voices[0].beats[0].chordId = '1';
  assert.equal(C.hasOwn(sc), true);
  C.put(sc, 0, new Map([[1, ch('D')]]), makeChord);
  C.clear(sc);
  assert.equal(st.bars[0].voices[0].beats[0].chord.name, 'Em');
});
