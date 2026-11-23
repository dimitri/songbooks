// The Mixer's and the player's auto-align (static/js/mixer-autoalign.js)
// for a song with a "lead:": the lead starts the recording — bar 0 on the
// music's start — and the first section starts one lead later, wherever
// the pattern would match best. Run by webassets' TestScripts (go test).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';

function load() {
  const ctx = { window: {}, Math, Float32Array, Float64Array, Array, Infinity, Number, Object, isFinite };
  vm.createContext(ctx);
  vm.runInContext(readFileSync(new URL('../static/js/mixer-autoalign.js', import.meta.url), 'utf8'), ctx);
  return ctx.window.SongbooksAutoAlign;
}

// A band from 0.5 s: a beat every 0.6 s (100 bpm), a stronger hit on each
// bar's first beat, loudest from 11 s on (where a voice might come in).
function recording() {
  const sr = 11025, dur = 40;
  const x = new Float32Array(sr * dur);
  for (let k = 0; 0.5 + k * 0.6 < dur; k++) {
    const t0 = 0.5 + k * 0.6;
    const amp = (k % 4 === 0 ? 0.9 : 0.4) * (t0 >= 11 ? 1 : 0.6);
    const i0 = Math.round(t0 * sr);
    for (let i = 0; i < 400 && i0 + i < x.length; i++) x[i0 + i] += amp * Math.sin(i * 0.9) * Math.exp(-i / 80);
    for (let i = 400; i < sr * 0.5 && i0 + i < x.length; i++) x[i0 + i] += 0.05 * Math.sin(i * 0.05);
  }
  return { x, sr };
}

test('with a lead, bar 0 is the music start and the first section one bar on', () => {
  const A = load();
  const barTicks = Array.from({ length: 41 }, (_, b) => b * 3840);
  const res = A.analyze(recording(), { tempo: 100, barTicks, onsets: [], barChords: [], firstFull: 1, lead: 1 });
  assert.ok(Math.abs(res.barTimes[0] - 0.5) < 0.15, `bar 0 at ${res.barTimes[0]}`);
  assert.ok(Math.abs(res.barTimes[1] - res.barTimes[0] - 2.4) < 0.15, `the lead bar is ${res.barTimes[1] - res.barTimes[0]} s`);
});
