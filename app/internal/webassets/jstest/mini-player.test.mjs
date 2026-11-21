// The mini player's plain logic (static/js/mini-player.js): the state kept
// in sessionStorage as a page is left, read back on the next one, which
// player is saved, whether a page's own player takes the recording over.
// Run by webassets' TestScripts (go test), or alone, from
// internal/webassets: node --test 'jstest/*.test.mjs'
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const NP = require('../static/js/mini-player.js');

const NOW = 1_800_000_000_000;
const media = (o = {}) => ({ currentTime: 0, duration: NaN, paused: true, ended: false, playbackRate: 1, volume: 1, readyState: 4, ...o });
const REC = '/song/12/audio.mp3?category=band&v=48213';
const info = (o = {}) => ({ key: REC, src: REC, title: 'Cobalt Morning', subtitle: 'Velvet Hour', page: '/setlists/4', ...o });

test('the same recording, whatever its size says', () => {
  assert.equal(NP.norm(REC), '/song/12/audio.mp3?category=band');
  assert.equal(NP.norm('/song/12/audio.mp3?v=3'), '/song/12/audio.mp3');
  assert.ok(NP.same(REC, '/song/12/audio.mp3?category=band&v='));
  assert.ok(NP.same(REC, '/song/12/audio.mp3?category=band'));
  assert.ok(!NP.same(REC, '/song/12/audio.mp3?category=youtube&v=48213'));
  assert.ok(!NP.same(REC, '/song/13/audio.mp3?category=band&v=48213'));
  assert.ok(NP.same('pcloud:7711', 'pcloud:7711'));
  assert.ok(!NP.same('pcloud:7711', 'pcloud:77'));
  assert.ok(!NP.same('', ''));
});

test('what the mini player may play: the app\'s paths and https links', () => {
  assert.ok(NP.safeSrc('/song/12/audio.mp3?category=band'));
  assert.ok(NP.safeSrc('https://c123.pcloud.com/dpZ/rehearsal.mp3'));
  for (const bad of ['//evil.example/x.mp3', '/\\evil', 'http://example.com/x.mp3', 'blob:https://app/1234', 'javascript:alert(1)', '', null, 3]) {
    assert.ok(!NP.safeSrc(bad), String(bad));
  }
});

test('a recording playing, kept as its page is left', () => {
  const s = NP.snapshot(media({ currentTime: 83.456, duration: 241.2, paused: false, playbackRate: 1.25, volume: 0.6 }), info(), NOW, true);
  assert.deepEqual(s, {
    v: 1, key: REC, src: REC, title: 'Cobalt Morning', subtitle: 'Velvet Hour', page: '/setlists/4', resolve: null,
    pos: 83.46, dur: 241.2, playing: true, rate: 1.25, volume: 0.6, at: NOW, left: true,
  });
});

test('nothing to keep: no recording, or one only this page can play', () => {
  assert.equal(NP.snapshot(media(), null, NOW, true), null);
  assert.equal(NP.snapshot(media(), info({ key: '' }), NOW, true), null);
  assert.equal(NP.snapshot(media(), info({ src: 'blob:https://app/77' }), NOW, true), null);
});

test('a mini player waiting for a tap still means to play, from where it was', () => {
  const s = NP.snapshot(media({ readyState: 0, currentTime: 0 }), info({ playing: true, pos: 61.5 }), NOW, true);
  assert.equal(s.playing, true);
  assert.equal(s.pos, 61.5);
});

test('the page linked back to is one of the app\'s; a pCloud link can be asked for again', () => {
  assert.equal(NP.snapshot(media(), info({ page: '//evil.example/' }), NOW).page, '');
  assert.equal(NP.snapshot(media(), info({ page: 'https://evil.example/' }), NOW).page, '');
  const pc = { kind: 'pcloud', host: 'eapi.pcloud.com', code: 'kZabc', file: 7711 };
  assert.deepEqual(NP.snapshot(media(), info({ key: 'pcloud:7711', src: 'https://c1.pcloud.com/x.mp3', resolve: pc }), NOW).resolve,
    { kind: 'pcloud', host: 'eapi.pcloud.com', code: 'kZabc', file: '7711' });
  assert.equal(NP.snapshot(media(), info({ resolve: { ...pc, host: 'evil.example/x?' } }), NOW).resolve, null);
  assert.equal(NP.snapshot(media(), info({ resolve: { ...pc, file: '1 OR 1' } }), NOW).resolve, null);
});

test('read back as it was kept', () => {
  const s = NP.snapshot(media({ currentTime: 30, duration: 200, paused: false }), info(), NOW, true);
  assert.deepEqual(NP.parse(JSON.stringify(s), NOW + 2000), s);
});

test('nothing read back from a damaged, foreign or stale state', () => {
  const ok = NP.snapshot(media({ currentTime: 30, duration: 200 }), info(), NOW, true);
  assert.equal(NP.parse(null, NOW), null);
  assert.equal(NP.parse('{', NOW), null);
  assert.equal(NP.parse('"x"', NOW), null);
  assert.equal(NP.parse(JSON.stringify({ ...ok, v: 2 }), NOW), null);
  assert.equal(NP.parse(JSON.stringify({ ...ok, src: 'javascript:alert(1)' }), NOW), null);
  assert.equal(NP.parse(JSON.stringify({ ...ok, at: 'yesterday' }), NOW), null);
  assert.equal(NP.parse(JSON.stringify(ok), NOW + NP.MAX_AGE + 1), null);
  assert.ok(NP.parse(JSON.stringify(ok), NOW + NP.MAX_AGE - 1));
});

test('read back within bounds', () => {
  const s = NP.parse(JSON.stringify({ v: 1, key: 'k', src: '/a.mp3', pos: 900, dur: 200, rate: 9, volume: -1, at: NOW, playing: 'yes', page: '//x' }), NOW);
  assert.equal(s.pos, 200);
  assert.equal(s.rate, 4);
  assert.equal(s.volume, 0);
  assert.equal(s.playing, false);
  assert.equal(s.page, '');
  assert.equal(s.title, '');
});

test('plays on at once only when its page was left playing', () => {
  assert.ok(NP.autoplay({ playing: true, left: true }));
  // Kept by the periodic save only: a copy of this tab, its page may still play.
  assert.ok(!NP.autoplay({ playing: true, left: false }));
  assert.ok(!NP.autoplay({ playing: false, left: true }));
  assert.ok(!NP.autoplay(null));
});

test('starts again where it was, not past the end', () => {
  assert.equal(NP.startPos({ pos: 42, dur: 200 }), 42);
  assert.equal(NP.startPos({ pos: 199.8, dur: 200 }), 199);
  assert.equal(NP.startPos({ pos: 12, dur: 0 }), 12);
  assert.equal(NP.startPos(null), 0);
});

test('played to its end: nothing to pick up', () => {
  assert.ok(NP.finished(media({ ended: true })));
  assert.ok(NP.finished(media({ currentTime: 199.9, duration: 200 })));
  assert.ok(!NP.finished(media({ currentTime: 120, duration: 200 })));
  assert.ok(!NP.finished(media({ currentTime: 3 })));
});

test('the player saved: the one playing, else the last that played', () => {
  const a = { media: media(), played: true, lastPlay: 10 };
  const b = { media: media(), played: true, lastPlay: 20 };
  const c = { media: media({ paused: false }), played: true, lastPlay: 5 };
  const idle = { media: media(), played: false, lastPlay: 0 };
  assert.equal(NP.pick([a, b, c]), c);
  assert.equal(NP.pick([a, b]), b);
  assert.equal(NP.pick([idle]), null);
  assert.equal(NP.pick([]), null);
});

test('a page whose player has the recording takes it over: no mini player', () => {
  const state = NP.parse(JSON.stringify(NP.snapshot(media({ currentTime: 30 }), info(), NOW, true)), NOW);
  const other = { owns: (k) => k === 'pcloud:1' };
  const mine = { owns: (k) => NP.same(k, '/song/12/audio.mp3?category=band&v=1') };
  const broken = { owns: () => { throw new Error('boom'); } };
  assert.deepEqual(NP.decide(state, [other, broken, mine]), { show: false, owner: mine });
  assert.deepEqual(NP.decide(state, [other, broken, {}]), { show: true });
  assert.deepEqual(NP.decide(state, []), { show: true });
  assert.deepEqual(NP.decide(null, [mine]), { show: false });
});

test('time as m:ss', () => {
  assert.equal(NP.fmt(0), '0:00');
  assert.equal(NP.fmt(83.9), '1:23');
  assert.equal(NP.fmt(3725), '62:05');
  assert.equal(NP.fmt(NaN), '–:––');
  assert.equal(NP.fmt(-1), '–:––');
});

// A media element as far as startAt goes.
function fakeMedia({ readyState = 4, play = () => Promise.resolve() } = {}) {
  const listeners = {};
  return {
    readyState, currentTime: 0, duration: 200, play,
    addEventListener(type, fn) { (listeners[type] ||= []).push(fn); },
    fire(type) { (listeners[type] || []).forEach((fn) => fn()); },
  };
}

test('startAt: from where it was, playing', async () => {
  const m = fakeMedia();
  assert.equal(await NP.startAt(m, 42, true), 'playing');
  assert.equal(m.currentTime, 42);
});

test('startAt: the browser wants a tap first', async () => {
  const blocked = Object.assign(new Error('no'), { name: 'NotAllowedError' });
  const m = fakeMedia({ play: () => Promise.reject(blocked) });
  assert.equal(await NP.startAt(m, 42, true), 'blocked');
  assert.equal(m.currentTime, 42);
});

test('startAt: another failure is one', async () => {
  const m = fakeMedia({ play: () => Promise.reject(Object.assign(new Error('gone'), { name: 'NotSupportedError' })) });
  await assert.rejects(NP.startAt(m, 1, true), /gone/);
});

test('startAt: paused, and placed again once the file is read', async () => {
  let played = false;
  const m = fakeMedia({ readyState: 0, play: () => { played = true; return Promise.resolve(); } });
  assert.equal(await NP.startAt(m, 42, false), 'paused');
  assert.equal(played, false);
  m.currentTime = 0; // a browser that ignored the start it was given
  m.fire('loadedmetadata');
  assert.equal(m.currentTime, 42);
});
