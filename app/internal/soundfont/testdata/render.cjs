// Plays a few notes with alphaTab's synthesizer, offline, from a
// soundfont, and says what came out (sf2_test.go's TestAlphaTabPlays):
//
//   node render.cjs alphaTab.min.js bank.sf2 24,33
//
// one track per program, four notes each; prints
// {"samples":…,"rms":…,"peak":…,"sha256":…}.
const fs = require('fs');
const crypto = require('crypto');
const [, , atPath, sfPath, programs] = process.argv;
const alphaTab = require(atPath);
alphaTab.Logger.logLevel = alphaTab.LogLevel.None;

// A synthesizer output that keeps what it's given.
function emitter() {
  const l = [];
  return { on: (f) => l.push(f), off: (f) => { const i = l.indexOf(f); if (i >= 0) l.splice(i, 1); }, trigger: (...a) => l.slice().forEach((f) => f(...a)) };
}
const chunks = [];
const out = {
  sampleRate: 44100, ready: emitter(), samplesPlayed: emitter(), sampleRequest: emitter(),
  open() {}, play() {}, pause() {}, destroy() {}, activate() {}, resetSamples() {},
  addSamples(s) { chunks.push(Float32Array.from(s)); },
  enumerateOutputDevices: async () => [], setOutputDevice: async () => {}, getOutputDevice: async () => null,
};
const synth = new alphaTab.synth.AlphaSynth(out, 500);
out.ready.trigger();
let failed = null;
synth.soundFontLoadFailed.on((e) => { failed = e; });
synth.loadSoundFont(new Uint8Array(fs.readFileSync(sfPath)), false);
if (failed) {
  console.log(JSON.stringify({ error: String(failed) }));
  process.exit(1);
}

const settings = new alphaTab.Settings();
const tracks = programs.split(',').map((p, i) => `\\track "T${i}" \\instrument ${p} . 0.3.4 2.3.4 3.2.4 5.2.4 |`).join(' ');
const imp = new alphaTab.importer.AlphaTexImporter();
imp.initFromString(`\\tempo 120 . ${tracks}`, settings);
const midi = new alphaTab.midi.MidiFile();
new alphaTab.midi.MidiFileGenerator(imp.readScore(), settings, new alphaTab.midi.AlphaSynthMidiFileHandler(midi)).generate();
synth.loadMidiFile(midi);
synth.metronomeVolume = 0;
synth.play();
for (let i = 0; i < 1000 && synth.state === 1; i++) out.sampleRequest.trigger();

const h = crypto.createHash('sha256');
let n = 0, sum = 0, peak = 0;
for (const c of chunks) {
  h.update(Buffer.from(c.buffer));
  for (const v of c) { n++; sum += v * v; peak = Math.max(peak, Math.abs(v)); }
}
console.log(JSON.stringify({ samples: n, rms: Math.sqrt(sum / Math.max(1, n)), peak, sha256: h.digest('hex') }));
