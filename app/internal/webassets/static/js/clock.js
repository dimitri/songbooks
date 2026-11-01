// Metronome-clock engine: the app's own tempo clock drives bar/section
// highlighting and the audible metronome, independent of any external
// player's position (per plan: no YouTube/Spotify position API dependency).
// AudioContext is created lazily, inside a user gesture, per iOS/mobile
// autoplay-unlock rules.

class Clock {
  constructor(bpm, beatsPerBar) {
    this.bpm = bpm || 120;
    this.beatsPerBar = beatsPerBar || 4;
    this.running = false;
    this._anchorPerf = 0; // performance.now() at the sync point
    this._anchorBeat = 0; // logical continuous beat at the sync point
  }

  beatDurationMs() { return 60000 / this.bpm; }

  currentBeat(nowMs = performance.now()) {
    if (!this.running) return this._anchorBeat;
    return this._anchorBeat + (nowMs - this._anchorPerf) / this.beatDurationMs();
  }

  currentBar(nowMs = performance.now()) {
    return Math.floor(this.currentBeat(nowMs) / this.beatsPerBar);
  }

  start(nowMs = performance.now()) {
    this._anchorBeat = this.currentBeat(nowMs);
    this._anchorPerf = nowMs;
    this.running = true;
  }

  pause(nowMs = performance.now()) {
    this._anchorBeat = this.currentBeat(nowMs);
    this.running = false;
  }

  // Jump to an absolute logical beat (e.g. the first beat of a bar),
  // preserving play/pause state.
  seekToBeat(beat, nowMs = performance.now()) {
    this._anchorBeat = Math.max(0, beat);
    this._anchorPerf = nowMs;
  }

  seekToBar(bar, nowMs = performance.now()) { this.seekToBeat(bar * this.beatsPerBar, nowMs); }

  // Tap-tempo, "determine" mode: feed taps while not playing to derive a
  // BPM. Median of recent intervals rejects one mistimed tap; a >2s gap
  // starts a fresh estimate rather than polluting the average.
  tapDetermine(nowMs = performance.now()) {
    this._taps = (this._taps || []).filter(t => nowMs - t < 2000);
    this._taps.push(nowMs);
    if (this._taps.length < 2) return null;
    const intervals = [];
    for (let i = 1; i < this._taps.length; i++) intervals.push(this._taps[i] - this._taps[i - 1]);
    intervals.sort((a, b) => a - b);
    const median = intervals[Math.floor(intervals.length / 2)];
    return Math.round(60000 / median);
  }

  // Tap-tempo, "re-sync" mode: snap to the nearest logical beat boundary
  // (not raw tap time) and blend the implied tempo (70/30) rather than
  // snapping instantly, so highlighting doesn't visibly jump.
  tapResync(nowMs = performance.now()) {
    const beat = this.currentBeat(nowMs);
    const nearest = Math.round(beat);
    const last = this._lastResyncAt;
    this._lastResyncAt = nowMs;
    if (last != null && nowMs - last < 2000 && nowMs - last > 100) {
      const impliedBpm = 60000 / (nowMs - last);
      if (impliedBpm > 20 && impliedBpm < 400) {
        this.bpm = this.bpm * 0.7 + impliedBpm * 0.3;
      }
    }
    this.seekToBeat(nearest, nowMs);
  }

  setBpm(bpm, nowMs = performance.now()) {
    this._anchorBeat = this.currentBeat(nowMs);
    this._anchorPerf = nowMs;
    this.bpm = bpm;
  }
}

// Accent pattern per time signature numerator — which beats (0-indexed)
// get the accented click. Compound meters (6/8, 9/8, 12/8) accent every
// third eighth note (the real main pulses — 2, 3 and 4 dotted-quarter
// beats respectively), not just beat 1; simple meters accent beat 1 only.
// Falls back to "beat 0 only" for anything else.
const ACCENTS = { 2: [0], 3: [0], 4: [0], 6: [0, 3], 9: [0, 3, 6], 12: [0, 3, 6, 9] };

class Metronome {
  constructor(clock) {
    this.clock = clock;
    this.sound = 'click';
    this.enabled = true;
    this._ctx = null;
    this._timer = null;
    this._nextBeat = 0;
  }

  _ensureCtx() {
    if (!this._ctx) this._ctx = new (window.AudioContext || window.webkitAudioContext)();
    if (this._ctx.state === 'suspended') this._ctx.resume();
    return this._ctx;
  }

  // soft: a compound-meter subdivision click (the 2nd/3rd eighth of a
  // dotted-quarter pulse) — audible, not silent, but clearly secondary to
  // the main pulse: quieter AND duller (lower pitch), not just quieter.
  // "Brighter" in a click sound is mostly its pitch/harmonic content, so
  // "less bright" means pulling the frequency down, not only the gain.
  _click(atTime, accent, soft) {
    const ctx = this._ctx;
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    let freq = { click: accent ? 1600 : 1100, beep: accent ? 1800 : 1200, wood: accent ? 500 : 350, tick: accent ? 2400 : 1800 }[this.sound] || 1000;
    let vol = accent ? 0.85 : 0.55;
    if (soft) { freq *= 0.62; vol *= 0.5; }
    const dur = this.sound === 'wood' ? 0.035 : 0.05;
    osc.type = this.sound === 'wood' ? 'triangle' : (this.sound === 'beep' ? 'sine' : 'square');
    osc.frequency.value = freq;
    gain.gain.setValueAtTime(vol, atTime);
    gain.gain.exponentialRampToValueAtTime(0.0001, atTime + dur);
    osc.connect(gain).connect(ctx.destination);
    osc.start(atTime);
    osc.stop(atTime + dur);
  }

  // A "flam" (a quiet extra tick just before the real beat) — the
  // metronome equivalent of a drummer's pickup into a fill. A real fill
  // runs for a bar or two BEFORE the change and lands together with the
  // band exactly ON the new section's downbeat — there is nothing useful
  // to announce once you're already there, only before. So this is the
  // entire cue: no special sound on arrival, just an audibly busier last
  // bar leading into it, growing busier the closer it gets.
  _pickup(atTime, accent, leadMs) {
    this._click(atTime - leadMs / 1000, false);
    this._click(atTime, accent);
  }

  // One-off count-in clicks, independent of the running clock — used both
  // before jumping to a section and (with a longer, drummer-chosen bar
  // count) before starting playback from a stop. Accents beat 1 of EVERY
  // bar, same as the running metronome, not just the very first click —
  // a 2-bar count-in that only accents once reads as 1-and-a-half bars to
  // anyone counting along.
  async countIn(beats, bpm, beatsPerBar = 4) {
    this._ensureCtx();
    const ctx = this._ctx;
    const beatDur = 60 / bpm;
    const accentBeats = ACCENTS[beatsPerBar] || [0];
    for (let i = 0; i < beats; i++) {
      this._click(ctx.currentTime + 0.02, accentBeats.includes(i % beatsPerBar));
      await new Promise(r => setTimeout(r, beatDur * 1000));
    }
  }

  start() {
    this._ensureCtx();
    this._nextBeat = Math.ceil(this.clock.currentBeat());
    // Lookahead scheduler: tick every ~25ms, schedule any beat boundary in
    // the next ~100ms against AudioContext.currentTime — immune to JS
    // event-loop jitter, unlike setTimeout-per-beat.
    this._timer = setInterval(() => this._tick(), 25);
  }

  stop() {
    if (this._timer) clearInterval(this._timer);
    this._timer = null;
  }

  _tick() {
    if (!this.enabled || !this.clock.running) return;
    const ctx = this._ctx;
    const lookaheadMs = 100;
    const nowBeat = this.clock.currentBeat();
    const horizonBeat = this.clock.currentBeat(performance.now() + lookaheadMs);
    while (this._nextBeat <= horizonBeat) {
      const beat = this._nextBeat;
      const beatIndexInBar = ((beat % this.clock.beatsPerBar) + this.clock.beatsPerBar) % this.clock.beatsPerBar;
      // customAccents (a Set of 0-indexed beat positions), when set,
      // overrides the time-signature-derived ACCENTS table — used by the
      // standalone practice metronome to let a player pick arbitrary
      // accent beats (e.g. 2 & 4 for backbeat practice), not just the
      // time signature's default downbeat(s).
      const accentBeats = this.customAccents || ACCENTS[this.clock.beatsPerBar] || [0];
      const accent = accentBeats.has ? accentBeats.has(beatIndexInBar) : accentBeats.includes(beatIndexInBar);
      const msUntil = (beat - nowBeat) * this.clock.beatDurationMs();
      const atTime = ctx.currentTime + Math.max(0, msUntil) / 1000;
      // How many beats until the next section starts, if that's within
      // this bar (0 = never checked past one bar out — there's nothing
      // useful to signal any earlier than that).
      let toChange = null;
      const starts = this.sectionStartBeats;
      if (starts) {
        for (let d = 1; d <= this.clock.beatsPerBar; d++) {
          if (starts.has(beat + d)) { toChange = d; break; }
        }
      }
      if (toChange === 1) this._pickup(atTime, accent, 45);
      else if (toChange === 2) this._pickup(atTime, accent, 80);
      else this._click(atTime, accent);
      // Compound meter (6/8, 9/8, 12/8): each "beat" here is a dotted
      // quarter — three eighth notes — and the visual beat-dots already
      // show that subdivision as 3 sub-ticks. The audio should actually
      // sound it too: "1 . . 4 . ." (downbeat, two soft clicks, next
      // pulse) rather than the 2nd/3rd eighth going by silently. No
      // pickup/accent variant for these — they're always the plain soft
      // click, the main pulse above is what carries the pickup cue.
      if (this.isCompound) {
        const subMs = this.clock.beatDurationMs() / 3;
        this._click(atTime + subMs / 1000, false, true);
        this._click(atTime + (2 * subMs) / 1000, false, true);
      }
      this._nextBeat++;
    }
  }
}
