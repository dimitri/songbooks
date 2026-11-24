# Guitar Pro files in songs — the model

Status: **built**, in two steps. The first (facts, `gp[...]` lines,
`content: gp`, the Score view, drift, the import writing references, the
Mixer playing the file, `cmd/gpmigrate`) still reads Riff JSON, so it can
ship before the songs move. The second (Riff JSON retired, the file
editor, docs, PDF notes) ships once production has no Riff JSON left.
Replaces the Riff editor of [editor-plan.md](editor-plan.md) phase 1 and
the rest of its phase 2.

Checked along the way: all 32 stored Guitar Pro files come back from
alphaTab's Guitar Pro 7 writer with the same tracks, notes, effects,
repeats, markers, backing tracks and sync points; their 296 bent notes
draw cleanly in alphaTab 1.8; every one of the 85 Riff voices in
production matches its file bar for bar, except two copied with the old
import's repeat reading (one bar too many, flagged as drift after the
move).

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

### 3.4 The full score next to the chart

A song can also keep its file *next to* its chart, with no `gp[...]`
lines: a full transcription of a song played from its chart. The chart is
laid over the file bar for bar, from one header line:

```
gp_start: 3
```

the file's bar, as played, where the chart's first bar starts (here the
file has two bars before it: a count-in, a silent lead-in). Absent, the
chart starts on the file's first bar.

A chart with a lead-in of its own (a `lead: N` header line, see the
format page: bars before its first section, in none) starts with its
lead: `gp_start` names the file bar the lead's first bar falls on, and
bar 1 of the chart is N bars later.
A file opening with two count-in bars and a chart written `lead: 2`
need no `gp_start` at all — the lead plays over the file's count-in.

The GP import checks the file against the chart before keeping it (its
step 3, *Check*): the chart's sections over the file's bars from
`gp_start`, their bar counts against the file's own section markers
(repeats and alternate endings played out; a marker played again through
a repeat is one section), the chords over each bar, time signature, tempo
and key. It shows two timelines on one bar axis (the chart's sections,
the file's), a row per section, and the chords and the file's notation of
the section picked. What the app can do is one tap: where the chart
starts (lining a section up with the file's marker, after the file's
silent bars, where the chords line up best when the file has no markers)
and the header facts; the rest is said in plain words ("the file's
Verse 2 has 2 more bars: add them to the chart's Verse 2, or edit the
file in Guitar Pro"). Nothing changes until *Import*.

The comparison is the server's (`gpalign.go`, `POST …/gp-check`) and
drift uses it for a song with no `gp[...]` lines (§4), so what the check
leaves and what "Out of step" shows after the import are the same items.
The GProTab.net search results use it too: each has a *Preview* (the
whole score, played, its facts next to the song's, how it lines up).

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
| Next to the chart (§3.4) | each section from `gp_start:` | the file's section marker there and its bar count, chords per bar, time signature |

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
- **`content: gp` songs open on the score** (and so does a course
  lesson's song with a file): the whole file in alphaTab, as a sheet of
  music — A4 pages, four bars a line where they fit — with a track
  picker and tab/staff. It plays in the song's player, the dock, like
  every other view: the song's recording (the file's backing track is
  one, `gp`), else the file through alphaTab's synthesizer (with
  GeneralUser GS, cut down to the file's instruments: soundfont.md), rendered
  once in the browser (`exportAudio`) and handed to the dock as its
  sound, waveform and all. alphaTab's player follows the dock
  (`PlayerMode.EnabledExternalMedia`): the cursor, a click on a note,
  bars dragged across as the dock's loop. The song view stays one tap
  away when the text has anything to show.
- **Score**, a new view, is there for any song with a file.
- The Drummer view and set lists work for both kinds.
- **Mixer:** a song with a file plays the file itself (every track, every
  effect, its own backing track) instead of alphaTeX rebuilt from voices.
  A song without one keeps playing its LilyPond voices. A file next to the
  chart (§3.4) is played from `gp_start`.
- **Where the file's score plays.** A file next to the chart (or inside
  it: not `content: gp`) is played, as a score, in the Score view only.
  The song's other views play its recordings, as a song with no file does
  (the file's own recording, `gp`, is one of them: the song's YouTube or
  band recording comes first, the file's when it is the only one). In the
  Score view the dock plays the file's sound (rendered by alphaTab's
  synthesizer, as for a song with no recording), even when the song has a
  recording; that recording follows it bar by bar, through its bar sync
  points or section markers and which bar of the chart each bar of the
  file is (`gp_start`, or the sections' `gp[...]` lines). A simplified
  mixer in the sheet bar sets them: *Recording* and *Score*, a mute and a
  level each, the full Mixer a tap away; a song with no recording plays
  the file's own (by its sync points), and a recording not lined up with
  the song's bars says so, Recording off. Leaving the Score view stops it
  and gives the dock back the song's recording. A `content: gp` song plays
  as before: its recording, else the file.

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
  version** with alphaTab's Guitar Pro 7 writer. Its palette is Guitar
  Pro 8's editing palette (gp-palette.js): the same groups in the same
  order — voice, lyrics, bar, note, effect, notation and automation
  symbols — with Guitar Pro's shortcuts, 122 buttons. Left out because
  alphaTab's model can't hold them: master volume and pan automations,
  pick scrapes, nested tuplets, sustain pedal marks, and design mode.
  Playback while editing. Earlier
  versions stay downloadable from History, so a save never loses the
  original bytes.
- **Guitar Pro itself**: download, edit, upload — the facts are
  re-extracted and the drift check runs.
- **LilyPond** `score[...]` voices stay text, edited in Source.

To check before relying on the writer: open each of the 32 stored files,
write it back, open the result, and compare the two models (tracks,
bars, notes, effects, automations, sync points).

## 8. Moving the existing songs

Each step is a production write, so it runs after a backup, one song at
a time, and only once you've approved it:

```
gpmigrate dump --db URL > songs.jsonl            # every song to move
node tools/gpmigrate/plan.cjs songs.jsonl --mixer URL > plan.jsonl
gpmigrate apply --db URL --actor NAME --dry-run < plan.jsonl
gpmigrate apply --db URL --actor NAME < plan.jsonl
```

`plan.cjs` needs a server that still reads Riff JSON for the songs with
no file (their Mixer alphaTeX becomes the new file); `apply` saves each
song through the app's own save path (a revision each), skips any song
edited since the dump, and fills in the facts of stored files that have
none.

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
   Once the author's own files turned up (Florent's pCloud folder), they
   replaced the made ones: `plan.cjs --originals map.json --current
   current.jsonl` finds each voice in the author's file instead, and
   stores it as the song's new file version (the made one stays in its
   history). All 34 matched bar for bar.
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
