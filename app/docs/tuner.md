# The tuner

A chromatic tuner at `/tuner`. The research behind it is in
`research_notes/Tuner and vocal warm ups/` (not in the repo); vocal
warm-ups were looked at and set aside.

## What the market does

- **The free baseline** (GuitarTuna, Fender Tune, BOSS, web tuners):
  auto string detection, a needle with cents, chromatic mode, A4
  calibration, reference tones.
- **What's paid:** the library of alternate tunings (GuitarTuna, about
  $20 a year; Fender's Tune Plus), Peterson's "sweetened" tunings.
- **Accuracy claimed:** ±1 cent (BOSS), ±0.5 (PolyTune), 0.1 (Peterson
  strobe).
- **A complaint that recurs:** auto detection picks the wrong string.
  Hence a "this string only" lock.
- **No product reviewed opens on a song's own tuning.** Every song's
  Guitar Pro file already says each track's tuning and capo.

## What's built (`/tuner`, `tuner.js`)

- **Pitch detection in the browser**, nothing leaves the phone: the
  McLeod Pitch Method (normalized square difference, first peak above
  90% of the highest, parabolic interpolation), about 80 lines of plain
  JS, no library, read 20 times a second.
- **Steady display:** median of the last five readings, eased; a note
  held within ±3 cents for 0.35 s is "In tune" (green, a short
  vibration on phones that have it).
- **Tunings:** the song's own first, when opened from a song
  (`/tuner?song=ID`: each Guitar Pro track's tuning and capo), then the
  ones below.
- **Strings:** one button each, the detected one outlined, green when in
  tune. Stopped, a tap plays the string's note (a plucked-string synth).
  Listening, a tap locks the tuner to that string.
- **Calibration** A = 425–455 Hz, remembered with the last tuning.
- **iOS:** the raw microphone (`echoCancellation`, `noiseSuppression`,
  `autoGainControl` all `false`); audio resumed within the tap; no
  reference tone while listening (iOS 26.4+ spoils a page's sound while
  the microphone is open, WebKit 311451); the microphone is released
  when the page is hidden, and the screen kept awake while listening.

## What it covers

Checked on generated tones (sine, sawtooth, and a weak fundamental with
strong overtones, as a phone's microphone hears a bass), within about a
cent: a 5-string bass's low B (30.9 Hz), bass E, a 7-string's low B, a
guitar's E2 to E4, a 12-string's octave strings, ukulele, and a capo'd
string.

- **Bass:** 4 strings (standard, drop D, E♭), 5 strings (low B, high
  C), 6 strings; an 8192-sample window below C2 (a low B is about 1,550
  samples a period at 48 kHz).
- **Guitar:** standard, drop D, E♭, D standard, drop C♯, drop C,
  C standard, drop B, B standard, DADGAD, open D, D minor, G, E, A, C,
  Nashville; 7 strings (standard, drop A).
- **12-string guitar and mandolin:** strings in pairs; a pair's button
  lights for either string, and a tap while listening tunes the low
  string, then its octave.
- **Ukulele** (GCEA, low G, baritone), **banjo** open G, **chromatic**.
- **Capo:** "Capo 1–9" shifts what the tuner listens for: tune with the
  capo on (it pulls strings a little sharp). The song's capo is picked
  when opened from a song.

## Where

A page of its own, like the metronome: in the desktop sidebar and the
phone's More menu, and from every song's tools (with that song's
tunings).

## Later, if wanted

- A strobe view for 0.1-cent precision.
- A coarse polyphonic check (strum, see which string is off).
