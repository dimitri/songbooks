// The level each track of a Guitar Pro 7/8 file plays at, as Guitar Pro
// plays it.
//
// Guitar Pro 8 keeps a track's volume (and pan) as an automation of its
// channel strip, at bar 1 (DSPParam_12, DSPParam_11), and may write 0 in
// the strip's own parameters, which is all alphaTab reads: such a track
// comes out of alphaTab's synthesizer silent (volume 0), its sound a
// flat line. GPLevels reads the automations from the file (a zip; its
// Content/score.gpif) and sets alphaTab's track levels from them.
//
//   GPLevels.read(bytes) → [{volume, balance}] by track, 0..1 or null where
//                          the file has no automation; [] for a file that
//                          isn't a Guitar Pro 7/8 zip.
//   GPLevels.apply(score, levels) → how many tracks it changed.
//   GPLevels.keep(written, original) → the bytes alphaTab wrote (written,
//                          a .gp zip), each track's channel-strip
//                          automations put back from the original file:
//                          alphaTab writes the strip's levels (from the
//                          model, levels applied: the file isn't silent in
//                          Guitar Pro) but drops its automations.
(function (root, factory) {
  if (typeof module === 'object' && module.exports) module.exports = factory();
  else root.GPLevels = factory();
})(typeof self !== 'undefined' ? self : this, function () {
  // A zip's entries, from its central directory: name, method, crc,
  // sizes and the (compressed) data. null for bytes that aren't a zip.
  function entries(bytes) {
    const v = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    // The end of the central directory: its signature, within the last 64 KB.
    let eocd = -1;
    for (let i = bytes.length - 22; i >= Math.max(0, bytes.length - 65557); i--) {
      if (v.getUint32(i, true) === 0x06054b50) { eocd = i; break; }
    }
    if (eocd < 0) return null;
    const count = v.getUint16(eocd + 10, true);
    let p = v.getUint32(eocd + 16, true);
    const dec = new TextDecoder();
    const out = [];
    for (let k = 0; k < count && p + 46 <= bytes.length; k++) {
      if (v.getUint32(p, true) !== 0x02014b50) return null;
      const e = {
        method: v.getUint16(p + 10, true), time: v.getUint16(p + 12, true), date: v.getUint16(p + 14, true),
        crc: v.getUint32(p + 16, true), csize: v.getUint32(p + 20, true), usize: v.getUint32(p + 24, true),
      };
      const nameLen = v.getUint16(p + 28, true), extraLen = v.getUint16(p + 30, true), commentLen = v.getUint16(p + 32, true);
      const local = v.getUint32(p + 42, true);
      e.name = dec.decode(bytes.subarray(p + 46, p + 46 + nameLen));
      p += 46 + nameLen + extraLen + commentLen;
      if (v.getUint32(local, true) !== 0x04034b50) return null;
      const start = local + 30 + v.getUint16(local + 26, true) + v.getUint16(local + 28, true);
      e.data = bytes.subarray(start, start + e.csize);
      out.push(e);
    }
    return out;
  }

  async function inflate(e) {
    if (e.method === 0) return e.data;
    if (e.method !== 8 || typeof DecompressionStream === 'undefined') return null;
    const out = new Blob([e.data]).stream().pipeThrough(new DecompressionStream('deflate-raw'));
    return new Uint8Array(await new Response(out).arrayBuffer());
  }

  // The bytes of one file in a zip (stored or deflated), null when absent.
  async function unzip(bytes, name) {
    const e = (entries(bytes) || []).find((x) => x.name === name);
    return e ? inflate(e) : null;
  }

  let crcTable = null;
  function crc32(b) {
    if (!crcTable) {
      crcTable = new Uint32Array(256);
      for (let n = 0; n < 256; n++) {
        let c = n;
        for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
        crcTable[n] = c >>> 0;
      }
    }
    let c = 0xffffffff;
    for (let i = 0; i < b.length; i++) c = crcTable[(c ^ b[i]) & 0xff] ^ (c >>> 8);
    return (c ^ 0xffffffff) >>> 0;
  }

  // A zip of these entries (each {name, method, crc, csize, usize, data},
  // data as stored in the zip).
  function zip(list) {
    const enc = new TextEncoder();
    const parts = [], central = [];
    let offset = 0;
    for (const e of list) {
      const n = enc.encode(e.name);
      const lh = new Uint8Array(30 + n.length);
      const lv = new DataView(lh.buffer);
      lv.setUint32(0, 0x04034b50, true);
      lv.setUint16(4, 20, true);
      lv.setUint16(8, e.method, true);
      lv.setUint16(10, e.time || 0, true);
      lv.setUint16(12, e.date || 0, true);
      lv.setUint32(14, e.crc, true);
      lv.setUint32(18, e.csize, true);
      lv.setUint32(22, e.usize, true);
      lv.setUint16(26, n.length, true);
      lh.set(n, 30);
      const ch = new Uint8Array(46 + n.length);
      const cv = new DataView(ch.buffer);
      cv.setUint32(0, 0x02014b50, true);
      cv.setUint16(4, 20, true);
      cv.setUint16(6, 20, true);
      cv.setUint16(10, e.method, true);
      cv.setUint16(12, e.time || 0, true);
      cv.setUint16(14, e.date || 0, true);
      cv.setUint32(16, e.crc, true);
      cv.setUint32(20, e.csize, true);
      cv.setUint32(24, e.usize, true);
      cv.setUint16(28, n.length, true);
      cv.setUint32(42, offset, true);
      ch.set(n, 46);
      parts.push(lh, e.data);
      central.push(ch);
      offset += lh.length + e.data.length;
    }
    const size = central.reduce((s, c) => s + c.length, 0);
    const end = new Uint8Array(22);
    const ev = new DataView(end.buffer);
    ev.setUint32(0, 0x06054b50, true);
    ev.setUint16(8, list.length, true);
    ev.setUint16(10, list.length, true);
    ev.setUint32(12, size, true);
    ev.setUint32(16, offset, true);
    const out = new Uint8Array(offset + size + 22);
    let p = 0;
    for (const x of [...parts, ...central, end]) { out.set(x, p); p += x.length; }
    return out;
  }

  // Each <Track>'s channel strip automations at the start of the song.
  function levelsOf(xml) {
    // The tracks, by their id (their order in the file); the master
    // track's (<MasterTrack>) is not one of them.
    const out = [];
    for (const m of xml.matchAll(/<Track\b([^>]*)>([\s\S]*?)<\/Track>/g)) {
      const id = Number((m[1].match(/\bid="(\d+)"/) || [])[1] ?? out.length);
      const strip = m[2].match(/<ChannelStrip\b[^>]*>([\s\S]*?)<\/ChannelStrip>/);
      const at = (param) => {
        if (!strip) return null;
        for (const a of strip[1].matchAll(/<Automation>([\s\S]*?)<\/Automation>/g)) {
          const f = (tag) => (a[1].match(new RegExp(`<${tag}>\\s*([^<]*?)\\s*</${tag}>`)) || [])[1];
          if (f('Type') !== param || Number(f('Bar') || 0) !== 0 || Number(f('Position') || 0) !== 0) continue;
          const x = parseFloat(f('Value'));
          return Number.isFinite(x) ? Math.max(0, Math.min(1, x)) : null;
        }
        return null;
      };
      out[id] = { volume: at('DSPParam_12'), balance: at('DSPParam_11') };
    }
    return Array.from(out, (l) => l || { volume: null, balance: null });
  }

  async function read(bytes) {
    try {
      const gpif = await unzip(bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes), 'Content/score.gpif');
      return gpif ? levelsOf(new TextDecoder().decode(gpif)) : [];
    } catch (e) {
      return [];
    }
  }

  // Each track's name and its channel strip's <Automations> block, in order.
  function stripsOf(xml) {
    return [...xml.matchAll(/<Track\b[^>]*>([\s\S]*?)<\/Track>/g)].map((m) => ({
      name: ((m[1].match(/<Name>\s*(?:<!\[CDATA\[([\s\S]*?)\]\]>|([^<]*))\s*<\/Name>/) || []).slice(1).find((x) => x !== undefined) || '').trim(),
      automations: ((m[1].match(/<ChannelStrip\b[^>]*>([\s\S]*?)<\/ChannelStrip>/) || [])[1] || '').match(/<Automations>[\s\S]*?<\/Automations>/)?.[0] || '',
    }));
  }

  // The written score.gpif, each track's strip given the original's
  // automations (by position when the tracks are the same, else by name).
  function keepXml(written, original) {
    const from = stripsOf(original);
    if (!from.some((t) => t.automations)) return written;
    const to = stripsOf(written);
    const same = from.length === to.length && from.every((t, i) => t.name === to[i].name);
    let k = 0;
    return written.replace(/<Track\b[^>]*>[\s\S]*?<\/Track>/g, (track) => {
      const i = k++;
      const src = same ? from[i] : from.find((t) => t.name && t.name === (to[i] || {}).name);
      if (!src || !src.automations) return track;
      return track.replace(/(<ChannelStrip\b[^>]*>)([\s\S]*?)(<\/ChannelStrip>)/, (all, open, body, close) =>
        (/<Automations>/.test(body) ? all : open + body + src.automations + close));
    });
  }

  async function keep(written, original) {
    try {
      const out = entries(written), orig = entries(original);
      const wi = out ? out.findIndex((e) => e.name === 'Content/score.gpif') : -1;
      const oe = orig && orig.find((e) => e.name === 'Content/score.gpif');
      if (wi < 0 || !oe) return written;
      const [w, o] = await Promise.all([inflate(out[wi]), inflate(oe)]);
      if (!w || !o) return written;
      const dec = new TextDecoder();
      const before = dec.decode(w);
      const after = keepXml(before, dec.decode(o));
      if (after === before) return written;
      const raw = new TextEncoder().encode(after);
      let data = raw, method = 0;
      if (typeof CompressionStream !== 'undefined') {
        data = new Uint8Array(await new Response(new Blob([raw]).stream().pipeThrough(new CompressionStream('deflate-raw'))).arrayBuffer());
        method = 8;
      }
      out[wi] = { ...out[wi], method, crc: crc32(raw), csize: data.length, usize: raw.length, data };
      return zip(out);
    } catch (e) {
      return written;
    }
  }

  // alphaTab's levels run 0..16, as its importer reads the strip's.
  function apply(score, levels) {
    let n = 0;
    (score.tracks || []).forEach((tr, i) => {
      const l = levels[i];
      if (!l || !tr.playbackInfo) return;
      if (l.volume !== null && tr.playbackInfo.volume !== Math.floor(16 * l.volume)) { tr.playbackInfo.volume = Math.floor(16 * l.volume); n++; }
      if (l.balance !== null) tr.playbackInfo.balance = Math.floor(16 * l.balance);
    });
    return n;
  }

  return { read, apply, keep, levelsOf, keepXml, unzip, entries, crc32 };
});
