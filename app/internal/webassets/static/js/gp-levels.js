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
(function (root, factory) {
  if (typeof module === 'object' && module.exports) module.exports = factory();
  else root.GPLevels = factory();
})(typeof self !== 'undefined' ? self : this, function () {
  // The bytes of one file in a zip (stored or deflated), null when absent.
  async function unzip(bytes, name) {
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
    for (let k = 0; k < count && p + 46 <= bytes.length; k++) {
      if (v.getUint32(p, true) !== 0x02014b50) return null;
      const method = v.getUint16(p + 10, true);
      const size = v.getUint32(p + 20, true);
      const nameLen = v.getUint16(p + 28, true), extraLen = v.getUint16(p + 30, true), commentLen = v.getUint16(p + 32, true);
      const local = v.getUint32(p + 42, true);
      const entry = dec.decode(bytes.subarray(p + 46, p + 46 + nameLen));
      p += 46 + nameLen + extraLen + commentLen;
      if (entry !== name) continue;
      if (v.getUint32(local, true) !== 0x04034b50) return null;
      const start = local + 30 + v.getUint16(local + 26, true) + v.getUint16(local + 28, true);
      const data = bytes.subarray(start, start + size);
      if (method === 0) return data;
      if (method !== 8 || typeof DecompressionStream === 'undefined') return null;
      const out = new Blob([data]).stream().pipeThrough(new DecompressionStream('deflate-raw'));
      return new Uint8Array(await new Response(out).arrayBuffer());
    }
    return null;
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

  return { read, apply, levelsOf, unzip };
});
