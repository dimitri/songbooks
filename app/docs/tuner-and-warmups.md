# A tuner, then vocal warm-ups

Both use the microphone and the same pitch detection. The tuner is built
first (`/tuner`); the warm-ups reuse its engine. The research behind
this is in `research_notes/Tuner and vocal warm ups/` (not in the repo).

## 1. The tuner

### What the market does

- **The free baseline** (GuitarTuna, Fender Tune, BOSS, web tuners):
  auto string detection, a needle with cents, chromatic mode, A4
  calibration, reference tones.
- **What's paid:** the library of alternate tunings (GuitarTuna, about
  $20 a year; Fender's Tune Plus), Peterson's "sweetened" tunings.
- **Accuracy claimed:** ±1 cent (BOSS), ±0.5 (PolyTune), 0.1 (Peterson
  strobe).
- **A complaint that recurs:** auto detection picks the wrong string.
  Hence a "this string only" lock.
- **No product reviewed links tunings to songs and setlists.** OnSong
  keeps a capo per song; bands keep a "tuning and capo map" by hand
  (Soundbrenner's advice). That's the opening: every song's Guitar Pro
  file already says each track's tuning and capo.

### What's built (`/tuner`, `tuner.js`)

- **Pitch detection in the browser**, nothing leaves the phone: the
  McLeod Pitch Method (normalized square difference, first peak above
  90% of the highest, parabolic interpolation), about 80 lines of plain
  JS, no library. A test tone 4 cents sharp of A2 reads "A₂ +4 ¢,
  110.3 Hz".
- **Steady display:** median of the last five readings, eased; a note
  held within ±3 cents for 0.35 s is "In tune" (green, a short
  vibration on phones that have it).
- **Tunings:** the song's own first, when opened from a song
  (`/tuner?song=ID`: each Guitar Pro track's tuning, with its capo
  reminder), then guitar (standard, drop D, E♭, D standard, drop C,
  DADGAD, open G, D, E), bass 4 and 5 strings, ukulele, chromatic.
- **Strings:** one button each, the detected one outlined, green when in
  tune. Stopped, a tap plays the string's note (a plucked-string synth).
  Listening, a tap locks the tuner to that string.
- **Calibration** A = 425–455 Hz, remembered with the last tuning.
- **iOS:** the raw microphone (`echoCancellation`, `noiseSuppression`,
  `autoGainControl` all `false`); audio resumed within the tap; no
  reference tone while listening (iOS 26.4+ spoils a page's sound while
  the microphone is open, WebKit 311451); the microphone is released
  when the page is hidden, and the screen kept awake while listening.
- **Bass:** a 8192-sample window when the tuning goes below C2 (a low B
  is about 1,550 samples a period at 48 kHz).
- **Where:** the sidebar (below Metronome) and every song's tools
  ("Tuner", next to Metronome).

### Next for the tuner

1. **Setlists: tuning and capo badges.** Each song of a setlist shows its
   tuning when it isn't standard, and its capo; between two songs whose
   tunings differ, a "retune: drop D → standard" line. The gig view opens
   the tuner on the next song's tuning. *No competitor does this.*
2. **The tuner over any page** (a sheet, like the recordings player):
   stays on the song or the setlist, no page change, the microphone held
   as long as the sheet is open (iOS forgets the permission on every page
   change in a Home Screen app).
3. **Strobe view** for the precise (0.1 cent display) and a "sweetened"
   offset per string, later.
4. **Polyphonic check** (strum all strings, see which is off) only as a
   coarse check against the known tuning; not a priority.

## 2. Vocal warm-ups

### What a good warm-up is

- **Semi-occluded vocal tract exercises first:** lip or tongue trills,
  humming, straw phonation. Best theory (Titze 2006, 2018: the straw's
  diameter matters most), thin clinical evidence (one small trial,
  Kapsner-Smith 2015). Then glides and sirens, 5-note scales, arpeggios
  (1-3-5-8-5-3-1), vowels, messa di voce, staccato.
- **5–10 minutes** is what most singers do; a 2022 Journal of Voice study
  found no rating difference between 5, 10 and 15 minutes.
- **Claims to avoid:** "prevents injury". Reviews find gains in comfort
  and some acoustic measures, with very mixed methods. A cool-down
  (Ragan 2018) has perceived, not measured, benefits.

### What apps do

- **Range-adaptive drills:** a range test, then piano patterns
  transposed to the singer, live pitch on screen (Vanido, Erol Singer's
  Studio, Tonelo; Swiftscales without pitch feedback).
- **Singing Carrots** (web) is the nearest: a free range test and pitch
  monitor, and song search by range with how far each key goes past it.
- **No app links a singer's range to a band's actual songs and keys.**

### The proposal

1. **Range test** per singer, kept on their profile: glide down, glide
   up, with the tuner's engine (smoothed for vibrato).
2. **A song's range** from its Guitar Pro vocal track (or its melody),
   shown on the song for each singer of the band: "fits", "1–2 semitones
   above your top", with the keys that fit them.
3. **A warm-up for today's setlist:** 6–10 minutes, built from the
   standard sequence (trills, glides, 5-note scales, arpeggios, vowels)
   and taken to the setlist's highest and lowest notes; piano
   accompaniment (Salamander Grand, CC-BY: credited), live pitch,
   headphones advised. A "warm up" step at the top of the gig view, and a
   2-minute cool-down after.
4. **Teachers** assign a warm-up in a lesson; it transposes to each
   student's range.

## Order

1. Ship the tuner as built; then setlist tuning and capo badges, then
   the tuner sheet.
2. Range test and song ranges (needs the vocal track's notes from the
   Guitar Pro facts).
3. The setlist warm-up with piano and live pitch.
4. Teacher-assigned warm-ups in courses.
