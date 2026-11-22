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
