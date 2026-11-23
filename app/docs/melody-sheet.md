# The melody sheet

The song page's **Melody** view (FR « Mélodie »): the lead voice's notes
on a staff, in the song's bars, with the song's lyrics under them. Both
are *estimated* from the recording — nothing is listened to for words —
and the page says so (the "Estimated" badge, the lines it is least sure
of drawn lighter). It downloads as MusicXML (MuseScore, Sibelius, Dorico)
and as a Guitar Pro file.

## Where it comes from

```
recording ─Demucs─▶ vocals part ─YIN─▶ frames ─segment─▶ notes (seconds, MIDI)     [background, once per split]
                                                         │
song's sync (points / section starts / analysis) ─▶ bar grid (played bars, lead)  [per request]
                                                         ▼
                                       quantize ─▶ bars of notes and rests (8ths, 16ths where dense, ties)
lyrics lines ─syllables─▶ lines × phrases (DP) ─▶ syllables on notes
                                                         ▼
                                    alphaTex (the page, alphaTab) · MusicXML · Guitar Pro (alphaTab's exporter)
```

1. **Notes** (`audioanalysis/melodynotes.go`, job `httpapi.RunMelodyNotes`,
   table `song_melody_notes`, migration 0126). The app reads each
   recording's vocals part once per split and analyzer
   (`MelodyNotesAnalyzer`), newest first, one at a time, as it does for
   the melody range (#143). The voice is tracked with the range's YIN
   (46 ms window, 23 ms hop), lone octave slips folded back, then cut into
   notes where it stops (a gap of two frames), where its pitch leaves the
   note it was on and stays away (0.85 semitone for 4 frames: a vibrato
   swings back sooner) and where its loudness dips between two syllables
   on one pitch. A note's pitch is the median of its middle frames (the
   scoop into it and the fall off it left out); notes under 90 ms are
   dropped; each note has a confidence (clarity × steadiness), under 0.5
   it's drawn lighter. Kept as `[start, end, midi, conf]` in seconds.
   A Guitar Pro backing track isn't split (0123), so has no melody.

2. **Bars** (`melody/grid.go`, `httpapi/melody_sheet.go melodyGrid`). The
   recording's bar grid is built exactly as the song page's player builds
   it (`audio-player.js rebuildGrid`): its per-bar sync points, else its
   section starts with the chart's bars spread between them (the chart's
   tempo before the first and after the last), else — nothing saved — the
   recording's analysis laid on the chart (`songimport.ProposeAutoSync`,
   the Sync sheet's estimate) when it isn't "unsure"; then the song's
   `lead:` bars placed before its first section (`lead-time.js`). The grid
   counts **played** bars: a `|: … :|` is two passes, a bare repeat of a
   section its own bars. So the score writes the song as it's sung, every
   pass in turn (verse 2's words differ from verse 1's), its bar numbers
   the player's (the cursor and the dock's counter agree); each section's
   label is written where it starts.

3. **Quantizing** (`melody/quantize.go`). Each note's start and end, as a
   fractional played bar, snapped to eighths — to sixteenths in a bar
   where two notes would land on the same eighth. A breath shorter than
   an eighth goes to the note before it (the line reads legato); a note
   over a bar line is tied; long notes are written as tied values on the
   beat (whole, dotted half, half, dotted quarter…). Time signatures per
   section (`time:` in a section) are followed. Notes before bar 0 or
   after the grid are dropped (counted in the response).

4. **Lyrics without speech recognition** (`melody/syllables.go`,
   `melody/lyrics.go`). The song's lyric lines in order (a bare repeat of
   a section repeats its lines), each cut into syllables by rule — French
   (vowel groups, consonant pairs that start a syllable: *tr, bl, ch, gn…*,
   the mute *e* sung before a consonant but not before a vowel or at the
   end of a line, *qu/gu*, tréma, *é* after a vowel) or English (vowel
   groups, silent final *e*, *-ed*, *-es*, *-le*, *ng/ck/gh* closing a
   syllable, contractions sung with their word); the language guessed
   from the lyrics. Text in parentheses is a backing vocal, left out.
   The voice's phrases are the analysis' voice phrases (#117) when there
   are some, else the notes grouped at rests of 0.35 s. A dynamic
   programme over (lines × phrases) lays the lines on the phrases, in
   order: a line takes one to three phrases (a breath mid-line; a long
   silence between them costs), a phrase may be left out (an ad-lib), a
   line may find none. Its costs: syllables against notes (more notes than
   syllables is a melisma, cheaper than the reverse), how fast that would
   be sung, and how much of the phrase falls outside the line's section as
   the grid places it (a bar's slack either side). Each line's syllables
   then go on its notes, one each; extra notes hold the last syllable
   (`_`, a melisma), extra syllables share the last note. A line's
   confidence is `exp(-cost/2.2)`; under 0.45 its words are drawn lighter.

5. **Writing it** (`melody/alphatex.go`, `melody/musicxml.go`). alphaTex
   for the page (one voice, treble clef — an octave down, `8vb`, when the
   voice's median is under C4 — the chart's key signature, its sections,
   tempo, the lyrics as alphaTex lays them: a token per note, `-` inside a
   word, `_` for a held or tied note, `+` for two syllables on one note);
   MusicXML 4.0 for download (unsure notes in parentheses, syllabic
   begin/middle/end). The Guitar Pro file is alphaTab's export of the
   score as drawn. The page's key (−/+, a coach's key, the player's Key)
   transposes the notes and the key signature (`?transpose=N`): the notes
   are in the recording's key, the signature the chart's.

## The page

`melody-view.js` loads alphaTab the first time the view is shown, draws
the score on A4 pages like the Score view (one line never split across
two pages), puts a shaded cursor on the bar the player is in (the score's
bars are the played bars, so repeats follow) and keeps it in sight while
playing; a click on a note plays from its bar. Until there's a melody it
says why: "available once the recording is split and synced" (no split,
or no sync and no confident analysis), the voice being read (the job
hasn't reached it), or too little singing. The view is offered on a song
with lyrics and a YouTube or band recording, and on any song whose melody
was found.

Editors correct a line: *Lyrics placement* lists the lines (section, the
bar its first note is in, how sure) with ◀ ▶ to start its syllables a
note earlier or later (`PUT /api/songs/{id}/melody/lyrics-shift`, table
`song_melody_lyric_shifts`). A correction is kept with the line's text:
when the line is edited it lapses.

## Limits

- **Melisma**: extra notes go to the end of the line. A run sung on one
  syllable mid-line puts the following syllables early until an editor
  shifts the line (one shift moves the whole line).
- **Ad-libs, backing vocals, doubled lead**: the split keeps them in the
  vocals part; a short ad-lib phrase is usually left out by the
  alignment, but harmonies sung with the lead confuse the pitch tracker
  (unsure notes, lighter).
- **Octave errors**: a frame or two an octave off is folded back; a whole
  phrase tracked an octave off (a breathy low voice, falsetto) isn't.
- **Slides, scoops, blue notes**: written as the nearest held note, or
  dropped when shorter than 90 ms; no glissando marks.
- **Rhythm**: on eighths (sixteenths where dense), no triplets or swing:
  a swung line reads as dotted eighths or as straight eighths.
- **Needs the sync**: without saved bars (or an analysis sure of the
  chart) there's no score; a wrong sync shifts everything. Notes before
  the song's bar 0 (a pickup into bar 1 is fine; an intro ad-lib before
  the lead isn't) are dropped.
- **Syllables by rule**: compounds and borrowed words are cut wrong now
  and then ("so-meone's"); the count is what matters for placing them,
  and it's right more often than the cut.
- **Key**: the key signature is the chart's `key:` (else the analysis');
  a recording in another key than the chart shows its own pitches under
  the chart's signature.
