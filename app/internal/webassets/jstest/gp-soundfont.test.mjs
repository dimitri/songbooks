// The soundfont a score plays with (static/js/gp-soundfont.js): the
// presets it asks the server for, and the URL. Run by webassets'
// TestScripts (go test), or alone, from internal/webassets:
// node --test 'jstest/*.test.mjs'
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const SF = require('../static/js/gp-soundfont.js');

const INSTRUMENT = 2, BANK = 4; // alphaTab's AutomationType
const beat = (autos) => ({ automations: autos.map(([type, value]) => ({ type, value })) });
function track(program, { drums = false, bank = 0, autos = [] } = {}) {
  return {
    playbackInfo: { program, bank },
    staves: [{ isPercussion: drums, bars: [{ voices: [{ beats: [beat([[INSTRUMENT, program]]), ...autos.map((a) => beat([a]))] }] }] }],
  };
}

test('a score asks for its tracks’ programs, sorted, once each', () => {
  const score = { tracks: [track(33), track(24), track(24)] };
  assert.deepEqual(SF.presetsOf(score), [[0, 24], [0, 33]]);
  assert.equal(SF.keyOf(SF.presetsOf(score)), '0-24_0-33');
});

test('drums: their kit and kit 0 (alphaTab’s fallback and click)', () => {
  assert.deepEqual(SF.presetsOf({ tracks: [track(24), track(0, { drums: true })] }), [[0, 24], [128, 0]]);
  assert.deepEqual(SF.presetsOf({ tracks: [track(25, { drums: true })] }), [[128, 0], [128, 25]]);
});

test('instruments a track changes to on the way, and its banks', () => {
  const score = { tracks: [track(24, { autos: [[INSTRUMENT, 30], [0, 120]] }), track(48, { bank: 8, autos: [[BANK, 0]] })] };
  assert.deepEqual(SF.presetsOf(score), [[0, 24], [0, 30], [0, 48], [8, 48]]);
});

test('the URL: the subset with the server’s bank, else Sonivox', () => {
  const score = { tracks: [track(24)] };
  SF.setBank('');
  assert.equal(SF.urlFor(score), SF.SONIVOX);
  SF.setBank('abc123');
  assert.equal(SF.urlFor(score), '/soundfonts/subset/0-24.sf2?sf=abc123');
  assert.equal(SF.urlFor({ tracks: [] }), SF.SONIVOX, 'nothing to play');
  const many = { tracks: Array.from({ length: 65 }, (_, i) => track(i)) };
  assert.equal(SF.urlFor(many), SF.SONIVOX, 'past the server’s limit');
  SF.setBank('');
});

test('bytesFor falls back to Sonivox when the subset fails', async () => {
  const asked = [];
  globalThis.fetch = async (url) => {
    asked.push(url);
    if (url.startsWith('/soundfonts/')) return { ok: false, status: 500 };
    return { ok: true, arrayBuffer: async () => new Uint8Array([82, 73, 70, 70]).buffer };
  };
  SF.setBank('abc123');
  const bytes = await SF.bytesFor({ tracks: [track(24)] });
  assert.deepEqual([...bytes], [82, 73, 70, 70]);
  assert.deepEqual(asked, ['/soundfonts/subset/0-24.sf2?sf=abc123', SF.SONIVOX]);
  SF.setBank('');
  delete globalThis.fetch;
});

test('update loads another subset only when the instruments need one', async () => {
  globalThis.fetch = async () => ({ ok: true, arrayBuffer: async () => new Uint8Array([1]).buffer });
  SF.setBank('abc123');
  const loaded = [];
  const handlers = [];
  const loadedHandlers = [];
  // alphaTab's API: its errors (a soundfont that failed among them), and
  // soundFontLoaded.
  const api = { loadSoundFont: (b, append) => loaded.push([b.length, append]), error: { on: (f) => handlers.push(f) }, soundFontLoaded: { on: (f) => loadedHandlers.push(f) } };
  const score = { tracks: [track(24)] };
  SF.attach(api, SF.urlFor(score));
  // Another error than the soundfont's: nothing.
  handlers.forEach((f) => f(new Error('An unknown error occurred when fetching the script.')));
  assert.deepEqual(loaded, []);
  // The subset alphaTab couldn't load: Sonivox, once.
  handlers.forEach((f) => f(new Error('Soundfont is not a valid Soundfont2 file')));
  handlers.forEach((f) => f(new Error('Soundfont is not a valid Soundfont2 file')));
  assert.deepEqual(loaded, [[SF.SONIVOX.length, false]]);
  // Once a soundfont is loaded, a later error isn't the soundfont's.
  const api2 = { ...api, error: { on: (f) => handlers.splice(0, handlers.length, f) } };
  SF.attach(api2, SF.urlFor(score));
  loadedHandlers.forEach((f) => f());
  handlers.forEach((f) => f(new Error('Soundfont is not a valid Soundfont2 file')));
  assert.equal(loaded.length, 1);
  assert.equal(await SF.update(api2, score), false, 'same instruments');
  score.tracks[0].playbackInfo.program = 25;
  assert.equal(await SF.update(api2, score), true);
  assert.deepEqual(loaded.slice(1), [[1, false]]);
  SF.setBank('');
  delete globalThis.fetch;
});
