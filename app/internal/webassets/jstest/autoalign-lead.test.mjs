// The Mixer's and the player's auto-align (static/js/mixer-autoalign.js)
// for a song with a "lead:": the lead starts the recording's music — bar 0
// on its first musical downbeat, past silence, talk or ambience before it
// — and the first section one lead later. Synthetic recordings: no audio
// files. Run by webassets' TestScripts (go test).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';

function load() {
  const ctx = { window: {}, Math, Float32Array, Float64Array, Array, Infinity, Number, Object, isFinite, String };
  vm.createContext(ctx);
  vm.runInContext(readFileSync(new URL('../static/js/mixer-autoalign.js', import.meta.url), 'utf8'), ctx);
  return ctx.window.SongbooksAutoAlign;
}
const A = load();
const SR = 11025;

// A seeded random, so the "speech" is the same every run.
function rng(seed) {
  let s = seed >>> 0;
  return () => { s = (s * 1664525 + 1013904223) >>> 0; return s / 4294967296; };
}
// A band: a beat every 0.6 s (100 bpm) from `from`, a stronger hit on each
// bar's first beat, a held chord under it.
function band(x, from, louder = 1) {
  for (let k = 0; from + k * 0.6 < x.length / SR; k++) {
    const i0 = Math.round((from + k * 0.6) * SR);
    const amp = (k % 4 === 0 ? 0.9 : 0.45) * louder;
    for (let i = 0; i < 400 && i0 + i < x.length; i++) x[i0 + i] += amp * Math.sin(i * 0.9) * Math.exp(-i / 80);
    for (let i = 0; i < 0.6 * SR && i0 + i < x.length; i++) x[i0 + i] += 0.06 * Math.sin((i0 + i) * 0.05);
  }
}
// Talk: noise in syllables of 80–250 ms, gaps of 50–300 ms — loud enough
// to pass for "sound", on no grid.
function talk(x, from, to, seed = 7) {
  const r = rng(seed);
  for (let t = from; t < to;) {
    const len = 0.08 + r() * 0.17, amp = 0.3 + r() * 0.3;
    const i0 = Math.round(t * SR), n = Math.round(len * SR);
    for (let i = 0; i < n && i0 + i < x.length; i++) x[i0 + i] += amp * (r() * 2 - 1) * Math.sin((Math.PI * i) / n);
    t += len + 0.05 + r() * 0.25;
  }
}
const barTicks = Array.from({ length: 41 }, (_, b) => b * 3840);
const detect = (x, lead) => A.analyze({ x, sr: SR }, { tempo: 100, barTicks, onsets: [], barChords: [], firstFull: lead, lead });
const near = (got, want, what, eps = 0.15) => assert.ok(Math.abs(got - want) < eps, `${what}: ${got.toFixed(2)}, want ${want}`);

test('silence, then the band: bar 0 where the band starts', () => {
  const x = new Float32Array(SR * 40);
  band(x, 3);
  const res = detect(x, 1);
  near(res.barTimes[0], 3, 'bar 0 (the lead)');
  near(res.barTimes[1] - res.barTimes[0], 2.4, 'the lead bar');
});

test('silence, talk, then the band: bar 0 where the band starts, not the talk', () => {
  const x = new Float32Array(SR * 40);
  talk(x, 3, 9);
  band(x, 9);
  // The first-sound reading (musicStart) already passes talk over: its
  // syllables never hold 0.3 s without a dip.
  near(A.testing.musicStart(A.testing.envelope(x, SR)), 9, 'the first sustained sound', 0.3);
  near(detect(x, 1).barTimes[0], 9, 'bar 0 (the lead)');
});

// Ambience: wind, a room, a crowd — continuous noise, swelling and
// fading, on no grid.
function ambience(x, from, to, seed = 3) {
  const r = rng(seed);
  let lp = 0;
  for (let i = Math.round(from * SR); i < Math.round(to * SR) && i < x.length; i++) {
    lp = 0.9 * lp + 0.1 * (r() * 2 - 1);
    x[i] += lp * (0.6 + 0.4 * Math.sin(i / SR * 0.9));
  }
}

test('ambience, then the band: bar 0 where the band starts, not the noise', () => {
  const x = new Float32Array(SR * 40);
  ambience(x, 1, 9);
  band(x, 9);
  const env = A.testing.envelope(x, SR);
  // The first-sound reading takes the noise for the music (before: the
  // lead started there); the periodic one doesn't.
  const first = A.testing.musicStart(env);
  assert.ok(first < 3, `the first sustained sound at ${first.toFixed(2)}: the noise`);
  const fit = A.testing.combFit(env, 0.6, first, 40);
  // Its first musical beat (a beat late at most, a soft first hit), the
  // bar's downbeat then found by the accent.
  near(A.testing.musicalStart(env, fit.period, fit.phase, first, 40), 9, 'the musical start', 0.65);
  near(detect(x, 1).barTimes[0], 9, 'bar 0 (the lead)');
});

test('talk over ambience, then the band', () => {
  const x = new Float32Array(SR * 40);
  ambience(x, 0.5, 10, 5);
  talk(x, 2, 9.6, 13);
  band(x, 10);
  near(detect(x, 1).barTimes[0], 10, 'bar 0 (the lead)');
});

test('a quiet first bar of music after talk still starts it', () => {
  const x = new Float32Array(SR * 40);
  talk(x, 0.5, 6, 11);
  band(x, 6, 0.5);
  band(x, 6 + 2.4, 0.5); // louder from the second bar on (both layers)
  near(detect(x, 1).barTimes[0], 6, 'bar 0 (the lead)');
});

test('no lead: unchanged (the pattern search)', () => {
  const x = new Float32Array(SR * 40);
  band(x, 0.5);
  const res = A.analyze({ x, sr: SR }, { tempo: 100, barTicks, onsets: [], barChords: [], firstFull: 0 });
  assert.ok(res.barTimes.length === 41);
});
