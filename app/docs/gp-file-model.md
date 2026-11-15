# Guitar Pro files in songs — the model

Status: **plan**. Replaces the Riff editor of [editor-plan.md](editor-plan.md)
phase 1 and the rest of its phase 2.

## 1. The model in one paragraph

A song is still its `.song` text: header, sections, chord grid, lyrics,
and LilyPond `score[...]` voices for short hand-written riffs. A song can
also have **one Guitar Pro file** attached (earlier versions kept, as
today). The text points into that file — a track, a bar range, tab or
staff — instead of copying its notes. Some facts live in both (tempo,
time signature, key, sections, chords, chord diagrams); the app checks
they agree and **flags any difference, never fixing it on its own**. A
song whose content is all in the file (an instrumental, one of Florent's
lessons) says so with one header line and opens on the file itself.

What goes away: **Riff JSON as a stored format** (`score[x]: {"Tuning":…}`),
the Riff tab editor of the Scores tab, `POST /api/score/riff`, and the GP
import's copying of notes into the text. The Go `tab.Riff` type stays,
but only as what a LilyPond voice is parsed into for drawing and playing;
it is never written back to a song.

## 2. Where things are today (production, 2026-11-15)

| | Songs |
|---|---|
| All songs | 258 |
| With a GP file attached | 32 (Belle of the Ball and 31 Florent lessons, 286–316) |
| With Riff JSON voices | 65 (85 voices) |
| — of which with a GP file | 31 |
| — of which with **no** GP file | 34 (the first Florent batch, 251–284) |
| With LilyPond voices | 8 |

Every Riff voice came from a GP file, through the import wizard or an
earlier import of the same lessons; the first batch just never kept the
file.

## 3. The song format

### 3.1 Pointing into the file

A section line, next to `grid:` and `score[...]:`:

```
[Verse 1]
grid: |E7 | A7 | E7 | . |
gp[Lead guitar]: 9-12
gp[Bass]: 9-12 staff
```

- `gp[Track]` names a track of the attached file, by its name in Guitar
  Pro (case and accents ignored), or by its number (`gp[#2]`) when names
  repeat. `gp:` with no track means every track: the whole sheet, as
  Guitar Pro's multi-track view shows it.
- The bars are **Guitar Pro's own bar numbers**, as shown in Guitar Pro,
  so a reader can check them there: `9-12`, `9`, or `1-4, 9-12`.
  Repeat signs inside the range are played as written, so a section's
  length is the range's *played* length.
- The last word, optional, is how to draw it: `tab`, `staff` (standard
  notation — the "sheet"), `tab+staff`, or `slash` (rhythm only). Left
  out, each reader gets their own choice for that instrument
  (`people.notation_prefs`, as today).
- A bare repeated section (`[Chorus]` with no content) repeats its
  `gp[...]` lines like it repeats its grid.
- A section can mix `gp[...]` and LilyPond `score[...]` lines: a riff
  written on the web next to the transcription from the file.

### 3.2 "Everything is in the file"

One header line:

```
title: Hommage à SRV
artist: Florent Passamonti
gp_source: 14_Hommage a SRV.gp
content: gp
```

`content: gp` means the file *is* the song: the page opens on the file's
own score (§5), and sections, grid and lyrics are optional. The text can
still hold a reference link, notes, and the copied header facts (tempo,
key, time) the library, search, set lists and the click track read.

### 3.3 Header facts

`gp_source:` stays (the attached file's name, visible and diffable in the
text). `tempo:`, `time:`, `key:`, `swing:` and `chord:` lines keep their
meaning; when a file is attached they are also checked against it (§4).

## 4. Facts in both places, and drift

### 4.1 What's compared

| Fact | In the text | In the file |
|---|---|---|
| Tempo | `tempo:` | first tempo automation |
| Time signature | `time:`, section `time:` | each bar's time signature |
| Key | `key:` | key signature of the first bar |
| Swing | `swing:` | triplet feel |
| Sections | `[Label]` and its bar count | markers (section names), bar counts |
| Chords | `grid:` bars | chord names on beats |
| Chord diagrams | `chord:` lines | the file's chord diagrams (§6) |
| References | `gp[...]` lines | the track exists, the bars exist, the range's played length equals the section's bar count |

### 4.2 How it's checked

Guitar Pro files are only read in the browser today (alphaTab). So when a
file is uploaded or saved, the browser also extracts its **facts** (the
right column above: tempo, time, key, swing, tracks with their
instrument and tuning, bar count, markers, chord names per bar, chord
diagrams, repeats) and sends them with the file. They're stored next to
it, per file version (`facts jsonb` on `song_gp_source_file` and its
versions table). The 32 files already stored get theirs from a one-off
Node script (alphaTab runs in Node).

On every save of the text or the file, the server compares the text
with the facts and stores the differences.

### 4.3 What a difference looks like

- Editors see **"Out of step with the Guitar Pro file"** on the song
  page and in the editor: one line per difference, "Tempo: 96 here,
  100 in the file", with the bar or section it's about.
- Each line offers **Use the file's** (edits the text), **Use this
  one** (edits the file, a new file version — see §7), or **Keep both**:
  a deliberate difference, such as a chart that simplifies the
  transcription's chords. *Keep both* is remembered for those two values
  and comes back if either side changes (`gp_drift_ack`: song, fact, a
  hash of each value, who, when).
- Nothing is ever fixed on its own, on upload or on save.
- History shows the drift a revision introduced or resolved.

## 5. Display

- **Default: the song, as always** — chart, lyrics, sections. A section
  with `gp[...]` lines draws those bars from the file, with alphaTab
  (one load of the file per page; each section renders its range with
  `display.startBar`/`barCount` and the chosen tracks), in the same
  place `score[...]` voices are drawn today.
- **`content: gp` songs open on the score**: the whole file in alphaTab,
  the Guitar Pro way — track picker, tab/staff, playback with the
  cursor, the file's backing track and its sync points, the speed and
  loop controls the Mixer already has. The song view stays one tap away
  when the text has anything to show.
- **Score**, a new view, is there for any song with a file.
- Stage mode, the Drummer view and set lists work for both kinds.
- **Mixer:** a song with a file plays the file itself (every track, every
  effect, its own backing track) instead of alphaTeX rebuilt from voices.
  A song without one keeps playing its LilyPond voices.

## 6. Chord diagrams from the file

Guitar Pro stores a chord diagram per chord name per track (alphaTab:
`staff.chords` — the fret per string, the first fret, the barres, and
whether the diagram is shown).

- The facts carry them; each becomes a `chord: Name Spec` line in our
  existing diagram grammar (`8:XX3(111)`), the name normalized with
  `chordsym`, strings turned the right way round (Guitar Pro numbers
  them from the lowest string, as it does for notes).
- Upload offers the diagrams the text doesn't have yet, ticked, and the
  ones that differ, unticked, like the header today.
- After that they're one more checked fact (§4): a diagram changed in
  Guitar Pro shows up as a difference, not silently.
- The chart's diagram row draws them like any `chord:` line; the score
  view shows Guitar Pro's own.

## 7. Editing

- **The text**: Source, Chart and the other tabs as today, plus `gp[...]`
  lines with completion (track names, bar numbers from the facts).
- **The file**: the web editor works on alphaTab's own model of the file
  (every track, voice and effect it holds) and saves a **new file
  version** with alphaTab's Guitar Pro 7 writer. Guitar Pro's keys,
  an effects panel, the multi-track view, bar-level time signature,
  tempo, repeats and markers, and playback while editing. Earlier
  versions stay downloadable from History, so a save never loses the
  original bytes.
- **Guitar Pro itself**: download, edit, upload — the facts are
  re-extracted and the drift check runs.
- **LilyPond** `score[...]` voices stay text, edited in Source.

To check before relying on the writer: open each of the 32 stored files,
write it back, open the result, and compare the two models (tracks,
bars, notes, effects, automations, sync points).

## 8. Moving the existing songs

Each step is a production write, so it runs after a backup, one
transaction per song, and only once you've approved it.

1. **The 31 songs with a file and Riff voices.** The wizard didn't
   store which bars fed which section, so find each voice's bars in its
   track by content. If the voice matches the file → replace it with
   `gp[Track]: a-b`. If it was changed on the web after the import → first
   write that change into the file (a new version, through the same
   path as *Export as Guitar Pro*), then replace it.
2. **The 34 lessons with no file (251–284).** Build a Guitar Pro file
   from each song's voices (alphaTeX → alphaTab → Guitar Pro 7 writer,
   as *Export as Guitar Pro* does), attach it with `gp_source:`, and
   replace the voices with references. Four of them have a later twin
   with the original file (Worried Life Blues 273/297, Black Magic Woman
   267/301, ES-335 Blues 279/307, BB King 284/310) — to look at
   together before deciding whether to keep both.
3. **The 8 LilyPond songs** don't change.
4. After the move, a check that no song body still holds Riff JSON.

## 9. What's removed

Once §8 is done:

- `tab.DecodeRiffJSON`, the JSON branch of `tab.DecodeScoreSource`
  (`riffjson.go`), and the Riff JSON tests;
- `tab-editor.js` and the Scores tab's Riff editor (the chord diagram
  builder stays), `POST /api/score/riff`;
- the GP import wizard's `extractRiff`/`putScore`: it keeps its timeline
  and its bar-to-section alignment, and writes `gp[...]` lines and the
  facts instead;
- the Riff paths of the Mixer, `songdiff` (`riffChanges` becomes a
  reference diff plus a file version diff), and the Riff-only parts of
  the chart rows and the PDF.

Until the file editor of §7 ships, notation can be edited on the web
only as LilyPond text.

## 10. Order of work

1. **Facts:** extract on upload, store per version, backfill the 32
   files. Chord diagrams from the facts (§6).
2. **`gp[...]` and `content: gp`** in the format: parse, write, validate,
   `songdiff`, the import wizard writing references.
3. **Display:** section ranges, the Score view, `content: gp` opening on
   it, the Mixer playing the file.
4. **Drift:** the comparison, the panel, *Keep both*.
5. **Move the songs** (§8), with your go-ahead for each production step.
6. **Remove Riff JSON** (§9).
7. **The file editor** (§7).
8. **PDF:** notation for `gp[...]` ranges. The PDF is drawn in Go, which
   can't read Guitar Pro files; options are drawing the ranges with
   alphaTab in Node, or printing chords and lyrics with a "see the
   score" line. Until then a PDF shows the chart and lyrics only for
   those sections.

## 11. Decisions taken

- Differences between the text and the file are flagged, never fixed
  automatically.
- Notation is edited on the web by editing the file itself.
- The 34 lessons without a file get one generated from their voices.
- One Guitar Pro file per song, earlier versions kept.

Still open, with a default unless you say otherwise:

- When a `gp[...]` line says `tab` or `staff`, does it override the
  reader's own choice? Default: no — the reader's own choice wins when
  they've made one, the line is the default for everyone else.
- A track renamed in Guitar Pro breaks `gp[Old name]` lines: shown as a
  difference with a one-click rename, not followed silently.
