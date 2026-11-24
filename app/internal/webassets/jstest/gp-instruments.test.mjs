// The instrument a Guitar Pro file's tracks play with
// (static/js/gp-instruments.js): what plays, whose choice wins, and the
// change alphaTab actually hears. Run by webassets' TestScripts (go
// test), or alone, from internal/webassets: node --test 'jstest/*.test.mjs'
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const GI = require('../static/js/gp-instruments.js');

const INSTRUMENT = 2; // alphaTab's AutomationType.Instrument
const beat = (autos) => ({ automations: autos.map(([type, value]) => ({ type, value })) });
function track(name, program, { drums = false, later = [] } = {}) {
  return {
    name, playbackInfo: { program },
    staves: [{ isPercussion: drums, bars: [{ voices: [{ beats: [beat([[INSTRUMENT, program], [0, 120]]), ...later.map((p) => beat([[INSTRUMENT, p]]))] }] }] }],
  };
}
const autos = (t) => t.staves[0].bars[0].voices[0].beats.flatMap((b) => b.automations.filter((a) => a.type === INSTRUMENT).map((a) => a.value));

test('setProgram changes the playback program and the automations alphaTab plays', () => {
  const t = track('Gtr', 24, { later: [30, 24] });
  assert.equal(GI.setProgram(t, 25), true);
  assert.equal(t.playbackInfo.program, 25);
  // The first beat's and the one going back to the track's own; the change
  // to a distortion guitar written later stays.
  assert.deepEqual(autos(t), [25, 30, 25]);
  assert.equal(GI.setProgram(t, 25), false, 'nothing to change');
  const d = track('Drums', 0, { drums: true });
  assert.equal(GI.setProgram(d, 25), false, 'drums keep their kit');
  assert.equal(GI.setProgram(track('X', 0), 200), false, 'not a GM program');
});

test('what plays: this person’s choice over the song’s over the file’s, for a track of the same name', () => {
  const v = 'v1';
  const s = GI.state(v);
  const score = { tracks: [track('Guitar', 24), track('Bass', 33), track('Drums', 0, { drums: true })] };
  GI.knowFile(v, score);
  assert.equal(GI.programFor(v, 0), 24);
  assert.equal(GI.chosen(v, 0), null);
  s.stored = [{ track: 0, name: 'Guitar', program: 25 }, { track: 1, name: 'Old bass', program: 34 }];
  assert.deepEqual(GI.chosen(v, 0), { program: 25, by: 'song' });
  assert.equal(GI.chosen(v, 1), null, 'a choice for a track of another name (the file replaced) doesn’t count');
  s.mine = { 0: { name: 'Guitar', program: 27 } };
  assert.deepEqual(GI.chosen(v, 0), { program: 27, by: 'mine' });
  assert.equal(GI.apply(v, score), true);
  assert.equal(score.tracks[0].playbackInfo.program, 27);
  assert.equal(score.tracks[1].playbackInfo.program, 33);
  assert.equal(GI.apply(v, score), false, 'applied once');
  // The file's own stays what the file said, not what was applied.
  assert.equal(GI.knowFile(v, score)[0].program, 24);
});

test('names: the short list’s, else the GM family and number', () => {
  assert.equal(GI.name(24), 'Nylon-string guitar');
  assert.equal(GI.name(31), 'Guitar (32)');
  assert.equal(GI.name(-1), 'MIDI');
  const all = GI.FAMILIES.flatMap(([, l]) => l.map(([p]) => p));
  assert.equal(new Set(all).size, all.length, 'no program twice');
  assert.ok(all.every((p) => p >= 0 && p <= 127));
});
