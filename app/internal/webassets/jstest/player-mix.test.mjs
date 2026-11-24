// The song player's mix (static/js/player-mix.js): what plays for a
// voice level, the voice's switch, a follower's seeks. Run by
// webassets' TestScripts (go test), or alone, from internal/webassets:
// node --test 'jstest/*.test.mjs'
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const Mix = require('../static/js/player-mix.js');

const TWO = ['vocals', 'no_vocals'];
const FIVE = ['vocals', 'no_vocals', 'drums', 'bass', 'other'];
const levels = (o) => (s) => (s in o ? o[s] : 100);

test('the recording as it is at 100', () => {
  assert.deepEqual(Mix.mode(TWO, levels({})), { main: '', mainLevel: 100, followers: [] });
  assert.deepEqual(Mix.mode(FIVE, levels({})), { main: '', mainLevel: 100, followers: [] });
});

test('the voice at 80: the backing and the voice beside it', () => {
  assert.deepEqual(Mix.mode(TWO, levels({ vocals: 80 })), { main: 'no_vocals', mainLevel: 100, followers: ['vocals'] });
  assert.deepEqual(Mix.mode(FIVE, levels({ vocals: 80 })), { main: 'no_vocals', mainLevel: 100, followers: ['vocals'] });
});

test('the voice off: the backing alone', () => {
  assert.deepEqual(Mix.mode(TWO, levels({ vocals: 0 })), { main: 'no_vocals', mainLevel: 100, followers: [] });
});

test('back to 100: the recording again', () => {
  let m = Mix.mode(TWO, levels({ vocals: 80 }));
  assert.equal(m.main, 'no_vocals');
  m = Mix.mode(TWO, levels({ vocals: 100 }));
  assert.equal(m.main, '');
});

test('another part down: the four parts, the voice at its level', () => {
  assert.deepEqual(Mix.mode(FIVE, levels({ drums: 0, vocals: 80 })), { main: 'other', mainLevel: 100, followers: ['vocals', 'bass'] });
  assert.deepEqual(Mix.mode(FIVE, levels({ other: 30 })), { main: 'other', mainLevel: 30, followers: ['vocals', 'drums', 'bass'] });
  // Split into the voice and the backing only: other parts can't be set.
  assert.deepEqual(Mix.mode(TWO, levels({ drums: 0 })), { main: '', mainLevel: 100, followers: [] });
});

test('no parts: the recording, whatever the levels', () => {
  assert.deepEqual(Mix.mode([], levels({ vocals: 0 })), { main: '', mainLevel: 100, followers: [] });
});

test('the switch: off, then on at the level it was', () => {
  let v = { voice: 100, back: 100 };
  v = Mix.toggleVoice(v);
  assert.deepEqual(v, { voice: 0, back: 100 });
  v = Mix.toggleVoice(v);
  assert.deepEqual(v, { voice: 100, back: 100 });
  // Set to 80 with the slider, then off and on: 80 again.
  v = Mix.setVoice(v, 80, true);
  assert.deepEqual(v, { voice: 80, back: 80 });
  v = Mix.toggleVoice(v);
  assert.equal(v.voice, 0);
  v = Mix.toggleVoice(v);
  assert.equal(v.voice, 80);
});

test('the switch turns the voice off, wherever the slider was dragged through', () => {
  // The old switch went between 100 and the last level under 100: after a
  // drag from 0 back up to 100 (… 90, 95, 100), "off" was 95.
  let v = { voice: 0, back: 100 };
  for (const n of [5, 30, 60, 90, 95]) v = Mix.setVoice(v, n, false);
  v = Mix.setVoice(v, 100, true);
  assert.equal(Mix.toggleVoice(v).voice, 0);
});

test('a drag down to 0 and the switch: back to where it was let go before', () => {
  let v = Mix.setVoice({ voice: 100, back: 100 }, 60, true);
  for (const n of [40, 20, 5, 0]) v = Mix.setVoice(v, n, false);
  v = Mix.setVoice(v, 0, true);
  assert.deepEqual(v, { voice: 0, back: 60 });
  assert.equal(Mix.toggleVoice(v).voice, 60);
});

test('levels are whole numbers between 0 and 100', () => {
  assert.equal(Mix.setVoice({ voice: 100, back: 100 }, 140, true).voice, 100);
  assert.equal(Mix.setVoice({ voice: 100, back: 100 }, -3, true).voice, 0);
  assert.equal(Mix.setVoice({ voice: 100, back: 100 }, '80', true).voice, 80);
});

test('an older save: its voiceOff is dropped', () => {
  assert.deepEqual(Mix.voiceFromSaved({ voice: 100, voiceOff: 95 }), { voice: 100, back: 100 });
  assert.deepEqual(Mix.voiceFromSaved({ voice: 80, voiceOff: 80 }), { voice: 80, back: 80 });
  assert.deepEqual(Mix.voiceFromSaved({ voice: 0, voiceOff: 0 }), { voice: 0, back: 100 });
  assert.deepEqual(Mix.voiceFromSaved({ voice: 0, voiceBack: 60 }), { voice: 0, back: 60 });
  assert.deepEqual(Mix.voiceFromSaved({}), { voice: 100, back: 100 });
  assert.deepEqual(Mix.voiceFromSaved(null), { voice: 100, back: 100 });
});

test('a follower is moved to the clock, but not again while it gets there', () => {
  assert.equal(Mix.shouldSeek({ time: 10, seeking: false, sentAt: 0 }, 10.05, 1000), false);
  assert.equal(Mix.shouldSeek({ time: 10, seeking: false, sentAt: 0 }, 10.3, 1000), true);
  assert.equal(Mix.shouldSeek({ time: 10, seeking: true, sentAt: 0 }, 10.3, 1000), false);
  assert.equal(Mix.shouldSeek({ time: 10, seeking: false, sentAt: 900 }, 10.3, 1000), false);
  assert.equal(Mix.shouldSeek({ time: 10, seeking: false, sentAt: 500 }, 10.3, 1000), true);
});
