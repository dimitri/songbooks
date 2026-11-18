# A singing teacher's community

What a voice teacher would need to run their students on Songbooks, the
way Florent runs his guitar course: what the others offer, what we have
(docs/stems-and-key.md), what's missing, and a warm-up editor with the
teacher's own recorded demo. Students recording themselves is the next
step, not this one. Research of November 2026; notes and every source in
`research_notes/Singing teachers/` (not in the repo).

## Who else

- **Practice apps for students**, pitch tracking at their core:
  [Singing Carrots](https://singingcarrots.com/studio) (about $120 a year;
  range test, songs found by range, teachers' own exercises, a free
  video "Studio" for teachers), Yousician, Simply Sing (every song moved
  to the student's tested range), [Singeo](https://www.singeo.com/)
  (Musora, $279 a year: video lessons, warm-up workouts, songs with or
  without the voice). Vanido closed; Smule and StarMaker are karaoke.
- **Closest to our song page**: [Moises](https://moises.ai/music-teacher-app/)
  (lead and backing vocals apart, key ±1 semitone steps, key and chord
  detection, synced lyrics, playlists for a class; $39.99 a year, and it
  refuses YouTube links), [Karaoke Version](https://www.karaoke-version.com/blog/1577-teaching-vocals-like-it-s-live-the-secret-tool-behind-real-progress.html)
  (re-recorded multitracks about $2.99 a song: lead voice, backing
  voices, click, count-in, ±4 semitones, sold to vocal teachers),
  Appcompanist (piano accompaniments and exercises, $99–149 a year).
- **How independent teachers sell**: courses on Teachable (Singing
  Success, Ramsey Voice), Skool communities ($9–26 a month, $99 with
  personal video feedback), studio tools (My Music Staff, Nova Music) for
  bookings and payments. In France, marketplaces (Superprof: about €38 an
  hour); no French tool for assigning warm-ups was found.

## What a singing teacher needs, most first

1. **Warm-ups and vocalises on piano**, the pattern moving up (or down) a
   semitone each time, within the student's range.
2. **Songs in the student's key.**
3. **The guide voice on, off, or anywhere between**, ideally keeping the
   backing voices.
4. **The teacher's own demos**, sung over the exercise.
5. **Feedback on the student's singing** (the paid tier everywhere).
6. Assignments and practice tracking.
7. A range test, suggesting each song's key.
8. A pitch line against the melody, as it's sung.
9. Lyrics following the recording.
10. Live lessons and a community: Zoom and Skool already, mostly.

## What we have now, and what's missing

Built (docs/stems-and-key.md): the key of any reference recording (YouTube
download, uploaded MP3, Guitar Pro recording) with the chords following,
the voice from off to full, the parts as Mixer lanes. With the community
pieces already there (members, paywall, courses of lessons, a video and
files per lesson, done and progress, branded spaces), that covers 2 and 3,
and the course side of 6.

Missing, in the order they'd matter:

- **The warm-up editor and the teacher's demo** (1 and 4): below.
- **A student's range** (7): a lowest and a highest note, measured with the
  tuner's pitch detection (tuner.js already hears a voice: "sing your
  lowest comfortable note, then your highest"), kept on the student; each
  song then suggests its key (its melody's span against the range), one
  click to Key.
- **Lyrics following the recording** (9): the songs have their lyrics and
  the recordings their bar grid; the *Lyrics only* view could light the
  line being sung, as the chart lights its bar.
- **Lead and backing voices apart** (3, fully): Demucs puts them together.
  Paid models (Moises, AudioShake) split them; an option, not a must.
- **Above 3 semitones** a voice sounds processed: shifting the
  accompaniment only (no_vocals) and leaving out the original voice is
  what singers practise with anyway.
- **A pitch line** (8) needs the melody: a Guitar Pro file's voice track,
  or one the teacher writes in the editor below. Later.
- **The student's recordings and the teacher's feedback** (5): the next
  feature set, as agreed.

## The warm-up editor

An exercise is a pattern the teacher writes once and the app plays in
every key, on piano, with the teacher's voice:

- **Pattern**: the notes as scale degrees (1 2 3 4 5 4 3 2 1), their
  lengths, a syllable or vowel per note ("ma", "ee", lip trill), the scale
  (major, minor, chromatic, arpeggio), and the piano under it: the melody
  doubled, a chord per bar, or nothing.
- **Travel**: start note, end note, up, down, or up then down, a semitone
  (or a tone) at a time, a prep chord before each repeat (the I, or V7 to
  I), the tempo, a breath between repeats.
- **Range**: the student's range (above) bounds the travel; the student can
  still move the start an octave by hand (users of auto-ranged warm-ups
  complain when it starts too low).
- **Drawn and played by what we have**: the pattern becomes alphaTex, so
  alphaTab draws it on a staff and AlphaSynth plays it with the soundfont's
  piano, the click and count-in included. No new audio engine.
- **Patterns to start from**: the common ones (five-note scale, 1-3-5-8-5-3-1,
  sirens, octave slides, staccato arpeggio), duplicated and changed, as
  Singio and ToneGym let teachers do.

### The teacher records the demo, once

- *Record the demo* plays the exercise's first repeat (count-in, prep
  chord, piano) and records the teacher's microphone over it, in the
  browser (`getUserMedia` with echo cancellation, noise suppression and
  gain control off, as the tuner does; headphones advised so the piano
  stays out). Listen back, record again, keep. Flat for Education's
  one-click recording over a score is the model for the flow.
- **The demo follows the exercise**: each repeat is the first one moved by
  so many semitones, so the demo is moved by the same with Signalsmith
  Stretch (the Key's shifter, formants kept). One recording, every key:
  no app reviewed does this (Cheryl Porter's has the student sing along
  with her recorded voice, but recorded key by key).
- **Played**: piano alone, piano and demo, demo alone, with the demo's
  volume like the Voice's. The student sings along, then without it.
- **Stored** like the recordings (Postgres for now, docs/stems-and-key.md),
  with its offset from the exercise's start.

### Where exercises live

A community's (or course's) **Warm-ups** page: the teacher's exercises in
order, a sequence played through as one session (5 minutes, 15 minutes),
and an exercise attached to a lesson like a song is. Original exercises
carry no rights question, unlike songs.

## Rights

Moises refuses YouTube links; Karaoke Version licenses for personal or
private-circle use; France's teaching exception covers school classrooms
under SACEM agreements, not a private teacher's paid community (no source
says otherwise). So: the parts and moved keys stay inside the community,
for its members, never downloadable (they're served only to who may see
the song, with no download link); a teacher adding a recording says they
may share it; and a takedown path. The YouTube download is the riskiest
part for a paid community: a teacher's own recordings, or licensed
backing tracks, are the clean way.

## Order

1. A student's range, and each song's suggested key. Small: the tuner and
   the Key exist.
2. The warm-up editor (alphaTex, alphaTab), the teacher's demo moved per
   repeat, the Warm-ups page and sessions.
3. Lyrics following the recording.
4. Then the next set: the students' recordings, the teacher's feedback.
