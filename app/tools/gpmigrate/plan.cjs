// Works out how each song with Riff JSON score lines moves onto
// "gp[Track]: bars" lines (docs/gp-file-model.md §8). Reads
// `gpmigrate dump` output, writes one plan line per song for
// `gpmigrate apply`, and a summary on stderr.
//
//   node tools/gpmigrate/plan.cjs songs.jsonl --mixer http://127.0.0.1:8102 > plan.jsonl
//
// - A song with a Guitar Pro file: each copied voice is found in the file
//   by its notes (every track, every place it could start) and replaced by
//   a line naming those bars. A voice that doesn't match its file bar for
//   bar is reported and left as it is.
// - A song without one: a file is made from its voices — the Mixer's
//   whole-song alphaTeX (--mixer, a server still reading Riff JSON),
//   written by alphaTab's Guitar Pro 7 writer — attached as
//   "<title>.gp", and each voice points at its section's bars in it.
//
// The file's facts (static/js/gp-facts.js) go with it, and with a stored
// file that has none yet.
'use strict';
const fs = require('fs');
const path = require('path');
const crypto = require('crypto');

const js = path.join(__dirname, '../../internal/webassets/static/js');
const alphaTab = require(path.join(js, 'vendor/alphatab/alphaTab.min.js'));
const GPFacts = require(path.join(js, 'gp-facts.js'));

const args = process.argv.slice(2);
const input = args.find((a) => !a.startsWith('--') && !/^https?:/.test(a));
const mixerAt = args.indexOf('--mixer');
const mixer = mixerAt >= 0 ? args[mixerAt + 1] : '';
if (!input) {
  console.error('usage: plan.cjs songs.jsonl [--mixer http://host:port]');
  process.exit(2);
}

const SECTION = /^\[([^[\]]+)\](\s*\(repeat\))?$/;
const RIFF = /^score\[([^\]]*)\]:\s*(\{.*)$/;

// The Riff voices of each section, by section index.
function riffVoices(body) {
  const out = [];
  let si = -1;
  for (const raw of body.split('\n')) {
    const line = raw.trim();
    if (line.startsWith('#')) continue;
    if (SECTION.test(line)) { si++; continue; }
    const m = si >= 0 && line.match(RIFF);
    if (!m) continue;
    try { out.push({ si, label: m[1].trim(), riff: JSON.parse(m[2]) }); } catch (e) { /* not JSON: LilyPond */ }
  }
  return out;
}

// rewrite replaces the Riff lines of body with the gp lines given per
// section and label, and adds header lines after the existing header.
function rewrite(body, lines, header) {
  const out = [];
  let si = -1;
  let headerEnd = -1;
  for (const raw of body.split('\n')) {
    const line = raw.trim();
    if (SECTION.test(line)) {
      if (si < 0) headerEnd = out.length;
      si++;
      out.push(raw);
      continue;
    }
    const m = si >= 0 && !line.startsWith('#') && line.match(RIFF);
    const key = m && `${si}\u0000${m[1].trim()}`;
    if (m && lines.has(key)) { out.push(lines.get(key)); continue; }
    out.push(raw);
  }
  if (header.length) {
    if (headerEnd < 0) headerEnd = out.length;
    let at = 0;
    for (let i = 0; i < headerEnd; i++) if (/^[a-z_-]+:/.test(out[i].trim())) at = i + 1;
    out.splice(at, 0, ...header);
  }
  return out.join('\n');
}

const fingerprintRiff = (bar) => (bar.Beats || []).map((b) => (b.Rest || !(b.Notes || []).length ? 'r' : b.Notes.map((n) => n.String + ':' + n.Fret).sort().join(','))).join(' ');
function fingerprintGP(staff, bi) {
  const n = staff.tuning.length;
  const v = staff.bars[bi].voices[0];
  return v.beats.map((b) => (b.isRest || !b.notes.length ? 'r' : b.notes.map((no) => (n + 1 - no.string) + ':' + no.fret).sort().join(','))).join(' ');
}

function trackLabel(tracks, ti) {
  const name = (tracks[ti].name || '').trim();
  const same = tracks.filter((t) => (t.name || '').trim().toLowerCase() === name.toLowerCase()).length;
  return name && same === 1 ? name : `#${ti + 1}`;
}

function rangesOf(bars) {
  const sorted = [...new Set(bars)].sort((a, b) => a - b);
  const r = [];
  for (const n of sorted) {
    const last = r[r.length - 1];
    if (last && n === last[1] + 1) last[1] = n; else r.push([n, n]);
  }
  return r.map(([a, b]) => (a === b ? String(a) : `${a}-${b}`)).join(', ');
}

const load = (bytes) => alphaTab.importer.ScoreLoader.loadScoreFromBytes(new Uint8Array(bytes), new alphaTab.Settings());

async function mixerOf(song) {
  if (!mixer) return null;
  const res = await fetch(`${mixer}/api/songs/${song.id}/mixer`);
  return res.ok ? res.json() : null;
}

async function withFile(song) {
  const bytes = Buffer.from(song.file, 'base64');
  const score = load(bytes);
  const played = GPFacts.played(score.masterBars).map((n) => n - 1);
  const mx = await mixerOf(song);
  const lines = new Map();
  const notes = [];
  for (const v of riffVoices(song.body)) {
    const want = v.riff.Bars.map(fingerprintRiff);
    const hitsAt = (ti, off) => {
      const st = score.tracks[ti].staves[0];
      let hits = 0;
      for (let k = 0; k < want.length && off + k < played.length; k++) if (fingerprintGP(st, played[off + k]) === want[k]) hits++;
      return hits;
    };
    // The import laid sections end to end from the file's first bar, each
    // voice from its own track: try exactly that first. Repeated bass
    // lines and silent bars match in many places, so only search the
    // whole file when it doesn't fit.
    const sec = mx && (mx.sections || []).find((x) => x.index === v.si);
    const named = GPFacts.findTrack(score.tracks, v.label);
    let best = { hits: -1 };
    if (sec && named >= 0 && sec.startBar + want.length <= played.length && hitsAt(named, sec.startBar) === want.length) {
      best = { ti: named, off: sec.startBar, hits: want.length };
    }
    if (best.hits < want.length) score.tracks.forEach((t, ti) => {
      const st = t.staves[0];
      if (st.isPercussion) return;
      for (let off = 0; off + want.length <= played.length; off++) {
        let hits = 0;
        for (let k = 0; k < want.length; k++) if (fingerprintGP(st, played[off + k]) === want[k]) hits++;
        if (hits > best.hits) best = { ti, off, hits };
      }
    });
    // The first imports played a "|: :|" with no opening bar from the
    // start of the file; such a voice is the whole file played that way.
    if (best.hits < 0 && want.length > played.length) {
      best = { ti: Math.max(0, GPFacts.findTrack(score.tracks, v.label)), off: 0, hits: -1, whole: true };
    }
    const label = `section ${v.si + 1}, ${v.label}`;
    if (best.whole) {
      notes.push(`${label}: ${want.length} bars copied with the old repeat reading, the file plays ${played.length}; pointing at the whole file`);
      lines.set(`${v.si}\u0000${v.label}`, `gp[${trackLabel(score.tracks, best.ti)}]: ${rangesOf(played.map((i) => i + 1))}`);
      continue;
    }
    if (best.hits < want.length * 0.8) {
      notes.push(`${label}: only ${best.hits} of ${want.length} bars match the file — left as it is`);
      continue;
    }
    if (best.hits < want.length) notes.push(`${label}: ${want.length - best.hits} of ${want.length} bars differ from the file (the copy dropped details); pointing at the file`);
    const bars = played.slice(best.off, best.off + want.length).map((i) => i + 1);
    lines.set(`${v.si}\u0000${v.label}`, `gp[${trackLabel(score.tracks, best.ti)}]: ${rangesOf(bars)}`);
  }
  const item = { id: song.id, version: song.version, body: rewrite(song.body, lines, []), notes };
  item.facts = GPFacts.extract(score);
  item.sha256 = crypto.createHash('sha256').update(bytes).digest('hex');
  return { item, replaced: lines.size };
}

async function withoutFile(song) {
  if (!mixer) throw new Error('needs --mixer to make a file');
  const res = await fetch(`${mixer}/api/songs/${song.id}/mixer`);
  if (!res.ok) throw new Error(`mixer: HTTP ${res.status}`);
  const mx = await res.json();
  if (!mx.alphaTex) throw new Error('mixer has no score');
  const settings = new alphaTab.Settings();
  const imp = new alphaTab.importer.AlphaTexImporter();
  imp.initFromString(mx.alphaTex, settings);
  const score = imp.readScore();
  score.title = song.title;
  score.artist = song.artist;
  score.tracks.forEach((t, i) => {
    const p = mx.tracks[i] && mx.tracks[i].program;
    if (p >= 0) t.playbackInfo.program = p;
    if (mx.tracks[i] && mx.tracks[i].name) t.name = mx.tracks[i].name;
  });
  const bytes = Buffer.from(new alphaTab.exporter.Gp7Exporter().export(score, settings));
  const written = load(bytes);
  const facts = GPFacts.extract(written);
  const lines = new Map();
  const notes = [];
  for (const v of riffVoices(song.body)) {
    const sec = (mx.sections || []).find((s) => s.index === v.si);
    const ti = GPFacts.findTrack(written.tracks, v.label);
    if (!sec || ti < 0) { notes.push(`section ${v.si + 1}, ${v.label}: not in the made file — left as it is`); continue; }
    lines.set(`${v.si}\u0000${v.label}`, `gp[${trackLabel(written.tracks, ti)}]: ${sec.startBar + 1}-${sec.startBar + sec.barCount}`);
  }
  const name = (song.title || 'song').replace(/[\\/:*?"<>|]+/g, ' ').trim() + '.gp';
  const item = {
    id: song.id, version: song.version, notes,
    body: rewrite(song.body, lines, [`gp_source: ${name}`]),
    file: { name, data: bytes.toString('base64'), facts },
  };
  return { item, replaced: lines.size };
}

(async () => {
  const songs = fs.readFileSync(input, 'utf8').trim().split('\n').filter(Boolean).map((l) => JSON.parse(l));
  let voices = 0, replaced = 0, made = 0;
  for (const song of songs) {
    voices += riffVoices(song.body).length;
    try {
      const { item, replaced: n } = song.file ? await withFile(song) : await withoutFile(song);
      if (!song.file) made++;
      replaced += n;
      process.stdout.write(JSON.stringify(item) + '\n');
      const what = song.file ? 'its file' : `a new file ${item.file.name}`;
      console.error(`${song.id} ${song.title}: ${n} voice(s) → ${what}${item.notes.length ? '\n    ' + item.notes.join('\n    ') : ''}`);
    } catch (e) {
      console.error(`${song.id} ${song.title}: NOT PLANNED — ${e.message}`);
    }
  }
  console.error(`\n${songs.length} songs, ${voices} Riff voices, ${replaced} replaced, ${made} files made`);
})();
