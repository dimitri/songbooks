// Each track's level in a Guitar Pro 8 file (static/js/gp-levels.js): its
// channel strip's automations, which alphaTab doesn't read (it reads the
// strip's parameters, 0 in such a file: the score sounded silent and its
// waveform was a flat line). Run by webassets' TestScripts (go test), or
// alone, from internal/webassets: node --test 'jstest/*.test.mjs'
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { deflateRawSync } from 'node:zlib';

const require = createRequire(import.meta.url);
const L = require('../static/js/gp-levels.js');

// A zip holding these files, stored or deflated (no CRCs: not checked).
function zip(files, deflate) {
  const enc = new TextEncoder();
  const locals = [], centrals = [];
  let offset = 0;
  for (const [name, text] of Object.entries(files)) {
    const n = enc.encode(name), raw = enc.encode(text);
    const data = deflate ? new Uint8Array(deflateRawSync(raw)) : raw;
    const lh = new Uint8Array(30 + n.length + data.length);
    const lv = new DataView(lh.buffer);
    lv.setUint32(0, 0x04034b50, true);
    lv.setUint16(8, deflate ? 8 : 0, true);
    lv.setUint32(18, data.length, true);
    lv.setUint32(22, raw.length, true);
    lv.setUint16(26, n.length, true);
    lh.set(n, 30);
    lh.set(data, 30 + n.length);
    const ch = new Uint8Array(46 + n.length);
    const cv = new DataView(ch.buffer);
    cv.setUint32(0, 0x02014b50, true);
    cv.setUint16(10, deflate ? 8 : 0, true);
    cv.setUint32(20, data.length, true);
    cv.setUint32(24, raw.length, true);
    cv.setUint16(28, n.length, true);
    cv.setUint32(42, offset, true);
    ch.set(n, 46);
    locals.push(lh);
    centrals.push(ch);
    offset += lh.length;
  }
  const cdSize = centrals.reduce((s, c) => s + c.length, 0);
  const end = new Uint8Array(22);
  const ev = new DataView(end.buffer);
  ev.setUint32(0, 0x06054b50, true);
  ev.setUint16(8, centrals.length, true);
  ev.setUint16(10, centrals.length, true);
  ev.setUint32(12, cdSize, true);
  ev.setUint32(16, offset, true);
  const out = new Uint8Array(offset + cdSize + 22);
  let p = 0;
  for (const part of [...locals, ...centrals, end]) { out.set(part, p); p += part.length; }
  return out;
}

const auto = (type, value, bar = 0) => `<Automation><Type>${type}</Type><Linear>false</Linear><Bar>${bar}</Bar><Position>0</Position><Visible>true</Visible><Value>${value}</Value></Automation>`;
// As Guitar Pro 8.1 writes it: the master track's strip, then the tracks',
// their volume (DSPParam_12) and pan (DSPParam_11) as automations at bar 1
// with 0 in the strip's own parameters.
const gpif = `<?xml version="1.0" encoding="utf-8"?><GPIF><MasterTrack><Tracks>0 1</Tracks><Automations>${auto('Tempo', '135 2')}</Automations>
<ChannelStrip><Parameters>0.5 0.5 0.5 0.5 0.5 0.5 0.5 0.5 0.5 0 0.5 0.5 0.8 0.5 0.5 0.5</Parameters></ChannelStrip></MasterTrack>
<Tracks><Track id="0"><Name><![CDATA[Guitare]]></Name><ChannelStrip version="E56"><Parameters>0.5 0.5 0.5 0.5 0.5 0.5 0.5 0.5 0.5 0 0.5 0.5 0 0.5 0.5 0.5</Parameters>
<Automations>${auto('DSPParam_11', '0.5')}${auto('DSPParam_12', '0.795')}${auto('DSPParam_12', '0.2', 8)}</Automations></ChannelStrip></Track>
<Track id="1"><Name><![CDATA[Basse]]></Name><ChannelStrip version="E56"><Parameters>0.5 0.5 0.5 0.5 0.5 0.5 0.5 0.5 0.5 0 0.5 0.25 0.7 0.5 0.5 0.5</Parameters></ChannelStrip></Track></Tracks></GPIF>`;

test('reads the tracks’ levels at the start, stored or deflated', async () => {
  for (const deflate of [false, true]) {
    const levels = await L.read(zip({ 'VERSION': '7.0', 'Content/score.gpif': gpif }, deflate));
    assert.deepEqual(levels, [{ volume: 0.795, balance: 0.5 }, { volume: null, balance: null }]);
  }
});

test('sets alphaTab’s levels where the file has them', () => {
  const score = { tracks: [{ playbackInfo: { volume: 0, balance: 8 } }, { playbackInfo: { volume: 11, balance: 4 } }] };
  assert.equal(L.apply(score, [{ volume: 0.795, balance: 0.5 }, { volume: null, balance: null }]), 1);
  assert.deepEqual(score.tracks.map((t) => t.playbackInfo), [{ volume: 12, balance: 8 }, { volume: 11, balance: 4 }]);
});

test('a file that isn’t a Guitar Pro 7/8 zip: no levels', async () => {
  assert.deepEqual(await L.read(new Uint8Array([0x46, 0x49, 0x43, 0x48, 0, 1, 2, 3])), []);
  assert.deepEqual(await L.read(zip({ 'other.txt': 'x' }, false)), []);
});

test('crc32', () => {
  assert.equal(L.crc32(new TextEncoder().encode('hello')), 0x3610a686);
});

// Saving: alphaTab writes the strip's levels from the model and drops the
// automations; keep() puts the original file's back, by track name when
// the tracks changed, and the file still reads (alphaTab, and here).
test('saving keeps the tracks’ automations', async () => {
  const alphaTab = require('../static/js/vendor/alphatab/alphaTab.min.js');
  const imp = new alphaTab.importer.AlphaTexImporter();
  imp.initFromString('\\title "T"\n\\track "Guitare"\n.\n:4 0.6 2.5 2.4 0.3 | :1 0.6 |', new alphaTab.Settings());
  const score = imp.readScore();
  const original = zip({ 'VERSION': '7.0', 'Content/score.gpif': gpif }, true);
  L.apply(score, await L.read(original).then((l) => [l[0]]));
  assert.equal(score.tracks[0].playbackInfo.volume, 12);
  const written = new alphaTab.exporter.Gp7Exporter().export(score, new alphaTab.Settings());
  // As alphaTab writes it: the level in the strip, no automation.
  assert.deepEqual(await L.read(written), [{ volume: null, balance: null }]);
  const kept = await L.keep(written, original);
  assert.deepEqual(await L.read(kept), [{ volume: 0.795, balance: 0.5 }]);
  const xml = new TextDecoder().decode(await L.unzip(kept, 'Content/score.gpif'));
  assert.match(xml, /<Parameters>[^<]* 0\.75 0\.5 0\.5 0\.5<\/Parameters><Automations>/);
  assert.equal((xml.match(/DSPParam_12/g) || []).length, 2); // bar 1 and bar 9, as in the original
  // Every other entry as alphaTab wrote it, the zip still sound.
  assert.deepEqual(L.entries(kept).map((e) => e.name), L.entries(written).map((e) => e.name));
  for (const e of L.entries(kept)) if (e.method === 0) assert.equal(L.crc32(e.data), e.crc);
  const again = alphaTab.importer.ScoreLoader.loadScoreFromBytes(kept, new alphaTab.Settings());
  assert.equal(again.tracks[0].name, 'Guitare');
  // Nothing to keep: the bytes as written.
  assert.equal(await L.keep(written, zip({ 'Content/score.gpif': '<GPIF><Tracks><Track id="0"><Name>Guitare</Name></Track></Tracks></GPIF>' }, false)), written);
});
