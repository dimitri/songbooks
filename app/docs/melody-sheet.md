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
lyrics lines ─syllables─▶ chords on the bars (anchors) ─▶ syllables on notes
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
   `melody/anchors.go`, `melody/lyrics.go`). The song's lyric lines in
   order (a bare repeat of a section repeats its lines), each cut into
   syllables by rule — French (vowel groups, consonant pairs that start a
   syllable: *tr, bl, ch, gn…*, the mute *e* sung before a consonant but
   not before a vowel or at the end of a line, *qu/gu*, tréma, *é* after a
   vowel) or English (vowel groups, silent final *e*, *-ed*, *-es*, *-le*,
   *ng/ck/gh* closing a syllable, contractions sung with their word); the
   language guessed from the lyrics. Text in parentheses is a backing
   vocal, left out.
   - **Anchors from the chart.** A line's `[Chord]` markers say which
     syllable each chord falls on; the section's grid, as played (repeats
     unrolled, a bare repeat given its source's bars), says in which bar
     and on which beat each chord comes. A dynamic programme lays the
     markers, in order, on the chord changes, in order (a change with no
     marker is skipped: instrumental bars; a marker whose chord isn't
     played where it could be is left unplaced). When the grid's chords
     come round again (a line's chord in several bars), the singing
     decides: the notes sung between two candidate places are counted
     against the syllables between the two markers, and a marker's beat
     must have a note on it. Each placed marker is a time in the recording
     (through the sync's grid).
   - **Lines without a chord placed** get their expected start between
     their section's placed lines, by syllables; a section with none
     spreads its lines over its bars.
   - **Placing.** Phrases end at a rest of an eighth or more (at the
     song's tempo, through the grid). Each chord's syllable goes on the
     note sung nearest its beat, from an eighth early (a pushed syllable)
     to half a bar late (a quarter when the sync is only an estimate); the
     syllables before a line's first chord (a pickup: "When the
     [G]night…") on the notes just before it, within a bar; between two
     chords, one syllable a note. After the last chord, the line takes the
     notes of its phrase, and the next phrases while syllables are left,
     never past the next line's first note nor into its expected place:
     extra notes hold the last syllable (a melisma), extra syllables share
     the last note. A line placed without chords takes the phrase starting
     nearest where it's expected (within a bar; half a bar when the sync
     is an estimate). A line's confidence is how well its syllables fit
     its notes, lower when its chords weren't all placed, lower still
     when it had none, and when the sync is an estimate — which the view
     then says ("placement depends on the sync — save the sync for better
     results").

5. **Writing it** (`melody/alphatex.go`, `melody/musicxml.go`). alphaTex
   for the page (one voice, treble clef — an octave down, `8vb`, when the
   voice's median is under C4 — the key signature ("Key signature"
   below), its sections,
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
says why (below, "Until it's ready"). The view is on every song.

### Until it's ready

The view is always there (hiding it was confusing): until the melody can
be drawn its tab is dimmed, its tooltip says why, and "42%" follows
"Mélodie" while the voice is split (on a phone, the View sheet's row);
the view itself shows the state in one line, a progress bar, and for an
editor a failed split's *Split the recording again*. The page is made
with the state (`data-status`); `melody-status.js` asks again while it
can change (`GET /api/songs/{id}/melody/status`, same rights as the song:
`melody_status.go`), after the server's `poll` seconds, half as long again
each time nothing moved (a minute at most), paused while the page is
hidden, stopped on a state that won't change by itself; when it turns
`ready` the sheet is drawn without a reload.

| state | when | says |
|---|---|---|
| `none` | no YouTube or band recording | add one |
| `unavailable` | not split, and no stems worker at work on this server (none seen for 15 min, no split started in the last 40: staging has none) | not available here |
| `queued` | its split is waiting | how many before it, and an estimate |
| `running` | its split is being made | the worker's % and what's left |
| `failed` | its split failed three times | an editor may queue it again |
| `notes` | split; its notes are being found (the app's job, woken by the split) | an estimate |
| `analysis` | notes, no sync saved, the recording's analysis (beats, bars) to come from a worker | — |
| `unsung` | too little singing | — |
| `sync` | notes, no bars for them | save the sync |
| `ready` | drawable | (the sheet) |

The **wait**: from the worker's progress when it reports it (`etaSource:
"worker"`: Demucs's elapsed time ÷ its share done, plus 8 % for the
parts' encoding), else estimated (`"estimate"`, said so): the
recording's size × the worker's median time per MB on its last 20 splits
(a split is "done" when its parts are saved, before the analysis), for
the splits before it (those running: what's left of theirs), its own,
and the melody's notes (their recent average). The page's first drawing
doesn't look for bars (a found melody is taken as ready; the view says
"sync" when it's opened if there are none).

**The worker** (`cmd/stemsd`, migration 0134): it notes itself in
`stem_workers` each time it looks for a job (`store.ClaimStemJob`, at
least once a minute); while Demucs runs, its progress bar on standard
error is read as it's written (`stems.WithProgress`) and every 5 % saved
in `song_audio_stem_jobs.progress` / `progress_at` (meaningful while the
job runs, `progress_at` after `started_at`). A worker that doesn't report
only loses the %: the estimate stands in.

### Key signature

The chart's `key:`, at the page's key (the songbook entry's or the
coach's key, the −/+: `?transpose=`), as the chords are. A chart without
one gets the key its **melody and chords** point to (`melody/key.go`,
Krumhansl–Schmuckler): the melody's pitch classes (each note's length ×
confidence, the recording's tuning offset taken out) and the chart's
chord tones as played (root, third, fifth, seventh or sixth, the bass),
each normalized and weighted 0.3 / 0.7, correlated with the
Krumhansl–Kessler profiles in the 24 keys, a key whose tonic chord ends
the chart (half as much: starts it) given 0.06 more. The page says "key
Eb, estimated from the melody and chords". Without notes or chords, the
recording's analysis' key; else none (C). An **editor** is told when the
chart's key and the guess write different signatures and the guess leads
the best key of another signature by 0.25 or more ("The melody and
chords sound like Cm, the chart says C: check its key"); a relative
major or minor writes the same signature: no disagreement for the sheet.

Measured on the demo songs with a melody (47; 23 with a `key:`), the
header against the guess:

| evidence | same key | relative | a fifth off | other |
|---|---:|---:|---:|---:|
| melody alone | 5 | 1 | 4 | 13 |
| chords alone | 16 | 0 | 5 | 2 |
| both, 0.5 / 0.5 | 13 | 0 | 6 | 4 |
| both, 0.3 / 0.7 | 16 | 0 | 4 | 3 |
| both, 0.3 / 0.7, last/first chord | **17** | 0 | 4 | 2 |

The melody alone is weak there because in 12 of the 23 the recording
isn't in the chart's key (its notes match the chords best moved by 1 to
11 semitones: another recording, a guitar tuned down); where they agree
(11), the melody alone found 4, the relative major once and the parallel
major or minor 3 times (blue thirds). The six misses with the chords: a
chart marked C whose chords are C minor's (the check is right), two
mixolydian songs (a ♭VII: read as the key a fourth up, or its relative),
a modulating song (verse in D minor, chorus in A minor), and two between
I and iii or V. The check would have told the editor about four of them
(leads 0.30–0.81), not the two closest calls (0.06, 0.12).

Editors correct a line: *Lyrics placement* lists the lines (section, the
bar its first note is in, how sure) with ◀ ▶ to start its syllables a
note earlier or later (`PUT /api/songs/{id}/melody/lyrics-shift`, table
`song_melody_lyric_shifts`). A correction is kept with the line's text:
when the line is edited it lapses.

## Limits

- **Melisma**: between two chords, extra notes go to the syllable before
  the next chord. A run sung on one syllable puts the following syllables
  early until the next chord's syllable, which is held to its beat; a
  line with one chord drifts more than one with several (an editor's
  shift moves the whole line).
- **Chords over the words** are what holds a line: a chart without them
  places its lines by its sections' bars and the phrases only.
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
- **Key**: the key signature is the chart's `key:` (else the melody's
  and chords' key, else the analysis'); a recording in another key than
  the chart shows its own pitches under the chart's signature. Modal and
  blues songs (a ♭VII, dominant sevenths everywhere) and songs that
  modulate are where the guess goes wrong.
