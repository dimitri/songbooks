// The Melody view's status poller (static/js/melody-status.js): which
// states it stops on, its waits (the server's, longer while nothing
// moves), its pause while the page is hidden, a failed request tried
// again, and the tab's note. Run by webassets' TestScripts (go test), or
// alone, from internal/webassets: node --test 'jstest/*.test.mjs'
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const MS = require('../static/js/melody-status.js');

// A clock of our own: timers run when the test says.
function clock() {
  const timers = [];
  return {
    timers,
    setTimeout(f, ms) { const t = { f, ms }; timers.push(t); return t; },
    clearTimeout(t) { const i = timers.indexOf(t); if (i >= 0) timers.splice(i, 1); },
    async fire() {
      const t = timers.shift();
      assert.ok(t, 'a timer to fire');
      await t.f();
      await new Promise((r) => setImmediate(r));
      return t.ms;
    },
  };
}

test('the states that stop it', () => {
  for (const s of ['none', 'unavailable', 'failed', 'unsung', 'sync', 'ready']) assert.ok(MS.done({ state: s }), s);
  for (const s of ['queued', 'running', 'notes', 'analysis']) assert.ok(!MS.done({ state: s }), s);
  assert.ok(MS.done(null));
});

test('the wait: the server\'s while things move, half as long again while not, a minute at most', () => {
  const run = (p) => ({ state: 'running', progress: p, poll: 5 });
  assert.equal(MS.nextDelay(null, run(0.1), 0), 5);
  assert.equal(MS.nextDelay(run(0.1), run(0.2), 5), 5);
  assert.equal(MS.nextDelay(run(0.2), run(0.2), 5), 8);
  assert.equal(MS.nextDelay(run(0.2), run(0.2), 50), 60);
  assert.equal(MS.nextDelay(run(0.2), run(0.2), 60), 60);
  assert.equal(MS.nextDelay({ state: 'queued', position: 2, poll: 15 }, { state: 'queued', position: 1, poll: 15 }, 30), 15);
});

test('the tab\'s note', () => {
  assert.equal(MS.tabNote({ state: 'running', progress: 0.423 }), '42%');
  assert.equal(MS.tabNote({ state: 'queued' }), '…');
  assert.equal(MS.tabNote({ state: 'ready' }), '');
  assert.equal(MS.tabNote({ state: 'failed' }), '');
});

test('it asks until ready, then stops', async () => {
  const c = clock();
  const answers = [
    { state: 'running', progress: 0.5, poll: 5 },
    { state: 'running', progress: 0.5, poll: 5 },
    { state: 'notes', poll: 5 },
    { state: 'ready' },
  ];
  const seen = [];
  const p = MS.poller({ fetch: async () => answers.shift(), onStatus: (st) => seen.push(st.state), setTimeout: c.setTimeout, clearTimeout: c.clearTimeout });
  p.start({ state: 'running', progress: 0.4, poll: 5 });
  assert.equal(await c.fire(), 5000);
  assert.equal(await c.fire(), 5000); // 40 → 50 %: moved
  assert.equal(await c.fire(), 8000); // still 50 %: longer
  assert.equal(await c.fire(), 5000); // notes: moved
  assert.deepEqual(seen, ['running', 'running', 'notes', 'ready']);
  assert.equal(c.timers.length, 0, 'no more asking once ready');
  assert.ok(!p.running());
});

test('a page that starts on a state that won\'t change never asks', () => {
  const c = clock();
  let asked = 0;
  const p = MS.poller({ fetch: async () => { asked++; return { state: 'ready' }; }, onStatus() {}, setTimeout: c.setTimeout, clearTimeout: c.clearTimeout });
  p.start({ state: 'unavailable' });
  assert.equal(c.timers.length, 0);
  assert.equal(asked, 0);
});

test('paused while the page is hidden, asked again when it\'s back', async () => {
  const c = clock();
  let hidden = true, asked = 0;
  const p = MS.poller({ fetch: async () => { asked++; return { state: 'ready' }; }, onStatus() {}, hidden: () => hidden, setTimeout: c.setTimeout, clearTimeout: c.clearTimeout });
  p.start({ state: 'queued', poll: 15 });
  await c.fire();
  assert.equal(asked, 0);
  assert.equal(c.timers.length, 0, 'nothing scheduled while hidden');
  hidden = false;
  p.resume();
  await new Promise((r) => setImmediate(r));
  assert.equal(asked, 1);
  assert.ok(!p.running());
});

test('a failed request is tried again, later', async () => {
  const c = clock();
  let n = 0;
  const p = MS.poller({
    fetch: async () => { n++; if (n === 1) throw new Error('502'); return { state: 'failed' }; },
    onStatus() {}, setTimeout: c.setTimeout, clearTimeout: c.clearTimeout,
  });
  p.start({ state: 'queued', poll: 15 });
  assert.equal(await c.fire(), 15000);
  assert.equal(c.timers.length, 1);
  assert.equal(c.timers[0].ms, 30000);
  await c.fire();
  assert.equal(n, 2);
  assert.ok(!p.running(), 'failed: stopped');
});

test('now() asks at once (a retry queued) and starts again after a stop', async () => {
  const c = clock();
  const answers = [{ state: 'queued', poll: 15 }];
  const p = MS.poller({ fetch: async () => answers.shift(), onStatus() {}, setTimeout: c.setTimeout, clearTimeout: c.clearTimeout });
  p.start({ state: 'failed' });
  assert.ok(!p.running());
  p.now();
  await new Promise((r) => setImmediate(r));
  assert.ok(p.running());
  assert.equal(c.timers.length, 1);
  assert.equal(c.timers[0].ms, 15000);
  p.stop();
  assert.equal(c.timers.length, 0);
});
