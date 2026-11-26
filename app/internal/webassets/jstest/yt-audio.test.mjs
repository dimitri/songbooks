// The video's audio under the YouTube video (static/js/yt-audio.js):
// what each state shows, the waits between asks, the length and the time
// left in words. Run by webassets' TestScripts (go test), or alone, from
// internal/webassets: node --test 'jstest/*.test.mjs'
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const YA = require('../static/js/yt-audio.js');

test('what each state shows', () => {
  let v = YA.view({ state: 'running', progress: 0.423, eta: 75 }, false);
  assert.equal(v.part, 'running');
  assert.equal(v.pct, '42%');
  assert.equal(v.bar, 0.423);
  assert.equal(v.eta, 'about 1 min left');
  v = YA.view({ state: 'queued' }, false);
  assert.equal(v.part, 'running');
  assert.equal(v.pct, '');
  assert.equal(v.eta, 'waiting its turn');
  v = YA.view({ state: 'queued', reason: 'blocked', retryIn: 300 }, true);
  assert.equal(v.eta, 'trying again in 5 min');
  assert.equal(YA.view({ state: 'queued', reason: 'blocked', retryIn: 55 }, true).eta, 'trying again in 1 min');
  assert.equal(YA.view({ state: 'queued', reason: 'blocked', retryIn: 10 }, true).eta, 'trying again in a moment');
  v = YA.view({ state: 'ready', duration: 222.4 }, false);
  assert.equal(v.part, 'ready');
  assert.equal(v.dur, '3:42');
  assert.equal(YA.view({ state: 'ready' }, false).dur, '');
  v = YA.view({ state: 'failed', reason: 'age_restricted' }, false);
  assert.equal(v.part, 'failed');
  assert.match(v.reason, /adults/);
  assert.match(YA.view({ state: 'failed', reason: 'unavailable' }).reason, /private, removed/);
  assert.match(YA.view({ state: 'failed', reason: 'blocked' }).reason, /refused/);
  assert.match(YA.view({ state: 'failed', reason: 'too_long' }).reason, /30 minutes/);
  assert.match(YA.view({ state: 'failed', reason: 'other' }).reason, /Couldn’t get/);
  // Removed, or none at all: the way to get one is an editor's; nothing
  // shows to anyone else, nor without a video.
  assert.equal(YA.view({ state: 'removed' }, true).part, 'removed');
  assert.equal(YA.view({ state: 'missing' }, true).part, 'removed');
  assert.equal(YA.view({ state: 'removed' }, false).part, '');
  assert.equal(YA.view({ state: 'none' }, true).part, '');
  assert.equal(YA.view(null, true).part, '');
});

test('asked again while queued or running only', () => {
  for (const s of ['queued', 'running']) assert.ok(YA.active({ state: s }), s);
  for (const s of ['ready', 'failed', 'removed', 'missing', 'none']) assert.ok(!YA.active({ state: s }), s);
  assert.ok(!YA.active(null));
});

test('the wait: the server\'s while it moves, half as long again while not, 30 s at most', () => {
  const run = (p) => ({ state: 'running', progress: p, poll: 2 });
  assert.equal(YA.nextDelay(null, run(0.1), 0), 2);
  assert.equal(YA.nextDelay(run(0.1), run(0.2), 2), 2);
  assert.equal(YA.nextDelay(run(0.2), run(0.2), 2), 3);
  assert.equal(YA.nextDelay(run(0.2), run(0.2), 25), YA.MAX_DELAY);
  assert.equal(YA.nextDelay({ state: 'queued', poll: 5 }, run(0), 5), 2);
});

test('a length and the time left, in words', () => {
  assert.equal(YA.clock(0), '0:00');
  assert.equal(YA.clock(59.6), '1:00');
  assert.equal(YA.clock(3723), '1:02:03');
  assert.equal(YA.etaText(-1), '');
  assert.equal(YA.etaText(undefined), '');
  assert.equal(YA.etaText(4), 'a few seconds left');
  assert.equal(YA.etaText(23), 'about 25 s left');
  assert.equal(YA.etaText(600), 'about 10 min left');
});
