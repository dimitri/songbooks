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
    // pattern: 'plain' (default) uses the single-timbre `sound` above,
    // accented per ACCENTS/customAccents. 'kick-hihat' and 'backbeat'
    // assign a voice per main-pulse PARITY instead (see _patternClick) —
    // independent of `sound`, which is ignored while one of these is
    // active.
    this.pattern = 'plain';
    this.enabled = true;
    this._ctx = null;
    this._timer = null;
    this._nextBeat = 0;
    // Practice modes (standalone metronome page only — see
    // metronome-page.js): 'none' (default, every beat sounds per the
    // normal accent/pattern rules above), 'gap' (play gapOnBeats, then
    // silence gapOffBeats, repeating — trains keeping time through a
    // silent stretch), 'random' (each beat independently has a
    // randomMuteBeatPct% chance of being silenced that pass, and each
    // compound-meter subdivision click independently has
    // randomMuteSubPct% — a different, complementary technique to the
    // fixed-pattern gap mode). Both leave accent/pattern/sound untouched;
    // they only decide whether a given click plays at all.
    this.practiceMode = 'none';
    this.gapOnBeats = 4;
    this.gapOffBeats = 4;
    this.randomMuteBeatPct = 30;
    this.randomMuteSubPct = 0;
  }

  // Whether the main click for this (absolute, continuous) beat index
  // should be silenced under the current practice mode.
  _muteMainBeat(beat) {
    if (this.practiceMode === 'gap') {
      const cycle = Math.max(1, this.gapOnBeats + this.gapOffBeats);
      const pos = ((beat % cycle) + cycle) % cycle;
      return pos >= this.gapOnBeats;
    }
    if (this.practiceMode === 'random') {
      return Math.random() * 100 < (this.randomMuteBeatPct || 0);
    }
    return false;
  }

  // Independently decided per subdivision click — random-mute only
  // (gap-click only ever silences whole main beats, not subdivisions).
  _muteSub() {
    if (this.practiceMode === 'random') return Math.random() * 100 < (this.randomMuteSubPct || 0);
    return false;
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
  _click(atTime, accent, soft, volScale = 1) {
    const ctx = this._ctx;
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    let freq = { click: accent ? 1600 : 1100, beep: accent ? 1800 : 1200, wood: accent ? 500 : 350, tick: accent ? 2400 : 1800 }[this.sound] || 1000;
    let vol = (accent ? 0.85 : 0.55) * volScale;
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

  // Synthesized kick: a sine oscillator with a fast downward pitch sweep
  // (the standard Web Audio "808 kick" technique) — needs its own pitch
  // envelope, unlike the fixed-frequency tones _click uses for
  // click/beep/wood/tick.
  _kick(atTime, vol = 1) {
    const ctx = this._ctx;
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.type = 'sine';
    osc.frequency.setValueAtTime(150, atTime);
    osc.frequency.exponentialRampToValueAtTime(45, atTime + 0.1);
    gain.gain.setValueAtTime(vol, atTime);
    gain.gain.exponentialRampToValueAtTime(0.0001, atTime + 0.12);
    osc.connect(gain).connect(ctx.destination);
    osc.start(atTime);
    osc.stop(atTime + 0.13);
  }

  // Synthesized hi-hat: filtered white noise, not a tone — a short burst
  // of random samples through a high-pass filter is the standard Web
  // Audio hi-hat technique, and is what makes it read as noisy/bright
  // rather than pitched.
  _hihat(atTime, vol = 0.5) {
    const ctx = this._ctx;
    const n = Math.ceil(ctx.sampleRate * 0.05);
    const buffer = ctx.createBuffer(1, n, ctx.sampleRate);
    const data = buffer.getChannelData(0);
    for (let i = 0; i < n; i++) data[i] = Math.random() * 2 - 1;
    const noise = ctx.createBufferSource();
    noise.buffer = buffer;
    const hp = ctx.createBiquadFilter();
    hp.type = 'highpass';
    hp.frequency.value = 7000;
    const gain = ctx.createGain();
    gain.gain.setValueAtTime(vol, atTime);
    gain.gain.exponentialRampToValueAtTime(0.0001, atTime + 0.018);
    noise.connect(hp).connect(gain).connect(ctx.destination);
    noise.start(atTime);
    noise.stop(atTime + 0.03);
  }

  // Pattern voice selection for 'kick-hihat'/'backbeat', by main-pulse
  // PARITY rather than a meter-specific lookup table. This generalizes
  // correctly across time signatures for free, because beatsPerBar here
  // is already normalized upstream to main-pulse count, not the raw
  // numerator (see timeSigBeats in handlers_pages.go / beatsForSig in
  // metronome-page.js: 6/8 -> 2 pulses, 12/8 -> 4 pulses, same as plain
  // 4/4's 4 beats). So "kick on pulse 1 & 3, hi-hat on 2 & 4" in 4/4
  // (0-indexed: kick on even pulses, hi-hat on odd) is literally the same
  // rule as 12/8's "kick on dotted-quarter pulses 1 & 3, hi-hat on 2 & 4"
  // (also 4 pulses) and 6/8's "kick on pulse 1, hi-hat on pulse 2" (2
  // pulses) — no per-signature special case needed, and it falls back
  // sensibly to any other beatsPerBar (3/4, 9/8, 5/4, ...) the same way.
  // Backbeat mode is the same parity rule with the even (kick) slot
  // silenced instead of voiced.
  _patternClick(atTime, beatIndexInBar, accent, volScale = 1) {
    if (this.pattern === 'kick-hihat') {
      if (beatIndexInBar % 2 === 0) this._kick(atTime, volScale);
      else this._hihat(atTime, 0.5 * volScale);
      return;
    }
    if (this.pattern === 'backbeat') {
      if (beatIndexInBar % 2 === 1) this._hihat(atTime, 0.5 * volScale);
      // Even pulses are silent by design — not quieter, absent.
      return;
    }
    this._click(atTime, accent, false, volScale);
  }

  // A "flam" (a quiet extra tick just before the real beat) — the
  // metronome equivalent of a drummer's pickup into a fill. A real fill
  // runs for a bar or two BEFORE the change and lands together with the
  // band exactly ON the new section's downbeat — there is nothing useful
  // to announce once you're already there, only before. So this is the
  // entire cue: no special sound on arrival, just an audibly busier last
  // bar leading into it, growing busier the closer it gets.
  _pickup(atTime, beatIndexInBar, accent, leadMs) {
    // The lead-in tick is always a crisp, audible cue — even over an
    // otherwise-silent backbeat pulse — since it's signaling "a section
    // change is coming," not standing in for the beat's own voice.
    if (this.pattern === 'kick-hihat' || this.pattern === 'backbeat') this._hihat(atTime - leadMs / 1000, 0.6);
    else this._click(atTime - leadMs / 1000, false);
    this._patternClick(atTime, beatIndexInBar, accent);
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
      const idx = i % beatsPerBar;
      this._patternClick(ctx.currentTime + 0.02, idx, accentBeats.includes(idx));
      await new Promise(r => setTimeout(r, beatDur * 1000));
    }
  }

  // One-off "give me the tempo, then let me play" cue: one full bar of
  // normal clicks, then a second bar fading linearly to silence — not a
  // hard stop, so the last click or two doesn't land as a jarring full-
  // volume note right before cutting out. Independent of the running
  // clock/scheduler (like countIn) so it works from a dead stop without
  // needing clock.start()/metronome.start() at all — used by the song
  // page's mini metronome widget.
  async playBarThenFade(bpm, beatsPerBar = 4) {
    this._ensureCtx();
    const ctx = this._ctx;
    const beatDur = 60 / bpm;
    const accentBeats = ACCENTS[beatsPerBar] || [0];
    const totalBeats = beatsPerBar * 2;
    for (let i = 0; i < totalBeats; i++) {
      const idx = i % beatsPerBar;
      const volScale = i < beatsPerBar ? 1 : Math.max(0, 1 - (i - beatsPerBar + 1) / beatsPerBar);
      this._patternClick(ctx.currentTime + 0.02, idx, accentBeats.includes(idx), volScale);
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
      // Practice-mode muting (standalone metronome only — toChange/pickup
      // never fires there since it has no song sections) always wins over
      // a plain click; a pickup cue (section change coming) still plays
      // even if this beat would otherwise be muted, since it's signaling
      // something time-critical.
      if (toChange === 1) this._pickup(atTime, beatIndexInBar, accent, 45);
      else if (toChange === 2) this._pickup(atTime, beatIndexInBar, accent, 80);
      else if (!this._muteMainBeat(beat)) this._patternClick(atTime, beatIndexInBar, accent);
      // Compound meter (6/8, 9/8, 12/8): each "beat" here is a dotted
      // quarter — three eighth notes — and the visual beat-dots already
      // show that subdivision as 3 sub-ticks. The audio should actually
      // sound it too: "1 . . 4 . ." (downbeat, two soft clicks, next
      // pulse) rather than the 2nd/3rd eighth going by silently. No
      // pickup/accent variant for these — they're always the plain soft
      // click, the main pulse above is what carries the pickup cue.
      if (this.isCompound) {
        const subMs = this.clock.beatDurationMs() / 3;
        if (!this._muteSub()) this._click(atTime + subMs / 1000, false, true);
        if (!this._muteSub()) this._click(atTime + (2 * subMs) / 1000, false, true);
      }
      this._nextBeat++;
    }
  }
}
