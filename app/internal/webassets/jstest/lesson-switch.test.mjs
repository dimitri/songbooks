// A lesson's one player (static/js/lesson-switch.js): the video or the
// recording, one at a time, the other paused when one is chosen or starts
// by itself. Run by webassets' TestScripts (go test), or alone, from
// internal/webassets: node --test 'jstest/*.test.mjs'
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const S = require('../static/js/lesson-switch.js');

// A video and a dock that say what they do, and what was paused.
function world(mode) {
  const w = { video: false, dock: false, log: [], modes: [] };
  w.sw = S.create({
    mode,
    pauseVideo: () => { if (w.video) w.log.push('video paused'); w.video = false; },
    pauseDock: () => { if (w.dock) w.log.push('dock paused'); w.dock = false; },
    onMode: (m) => w.modes.push(m),
  });
  // Each starting by itself, as YouTube's controls or the dock's play do.
  w.playVideo = () => { w.video = true; w.sw.videoPlaying(); };
  w.playDock = () => { w.dock = true; w.sw.dockPlaying(); };
  return w;
}
const never = (w) => assert.ok(!(w.video && w.dock), 'both playing');

test('it starts as asked, play along by default', () => {
  assert.equal(world('watch').sw.mode, 'watch');
  assert.equal(world().sw.mode, 'play');
  assert.equal(world('anything').sw.mode, 'play');
});

test('choosing one pauses the other', () => {
  const w = world('watch');
  w.playVideo();
  assert.equal(w.sw.set('play'), true);
  assert.equal(w.video, false);
  assert.deepEqual(w.log, ['video paused']);
  w.playDock();
  w.sw.set('watch');
  assert.equal(w.dock, false);
  assert.deepEqual(w.modes, ['play', 'watch']);
  // The same again: nothing changes, nothing more said; still paused.
  assert.equal(w.sw.set('watch'), false);
  assert.deepEqual(w.modes, ['play', 'watch']);
});

test('the video started from its own controls takes the player', () => {
  const w = world('play');
  w.playDock();
  w.playVideo();
  never(w);
  assert.equal(w.sw.mode, 'watch');
  assert.equal(w.dock, false);
  assert.deepEqual(w.log, ['dock paused']);
});

test('the dock started (its play, a key) takes the player back', () => {
  const w = world('watch');
  w.playVideo();
  w.playDock();
  never(w);
  assert.equal(w.sw.mode, 'play');
  assert.equal(w.video, false);
});

test('never two at once, whatever the order', () => {
  const w = world('watch');
  const steps = [w.playVideo, w.playDock, () => w.sw.set('watch'), w.playVideo, () => w.sw.set('play'), w.playDock, w.playVideo, w.playVideo, w.playDock];
  for (const s of steps) { s(); never(w); }
});
