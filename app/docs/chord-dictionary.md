# Chord dictionaries and a voicing finder (guitar, ukulele)

Status: **notes for later** (2026-11-24). Nothing is built from this
document yet.

The idea is a chord dictionary per instrument and tuning, and a finder
that answers two questions: "how do I play this chord here?" and "what
is this chord I'm holding?" It would be wired into the song page, the
editor and the PDF.

The key, the capo and the place's key it builds on are described in
[key-and-transpose.md](key-and-transpose.md).

## 1. What there is today

- **Chord diagrams written by hand**: `chord: Name Spec` header lines.
  `chord[K]:` gives the diagrams for another key, and `Song.DiagramsFor`
  and `DiagramSets` read them.
  - The Spec grammar (`internal/chorddiagram`) is the LaTeX `songs`
    package's `\gtab` grid: `[BaseFret:]` then one token per string,
    lowest string first.
    - `X` is a muted string and `O` an open one.
    - A digit 1–9 is a row in the box.
    - Parentheses mark a barre, so `7:XX(3121)` is valid.
  - The grammar has **no fingers, no instrument and no tuning**. The
    number of strings is the number of tokens.
  - On staging only 3 of 258 songs have any `chord:` line.
- **Rendering**:
  - `chorddiagram.RenderSVG` draws the legend row under the title on
    the song page.
  - `pdfnative.drawChordLegend` draws it in the PDF.
  - `POST /api/chord-diagrams/preview` (`chorddiagram_preview.go`) is the
    preview endpoint.
  - Transposed on the page (`song.js`, `applyDiagrams`), the row swaps
    to the `chord[K]:` set for the shown key. If there is none, it shows
    "No chord diagrams for %s yet.", which is the gap a finder would
    fill.
- **The editor's diagram builder** (`score-editor.js`, the Scores tab):
  - a 4–6 string grid with 4 rows;
  - a barre is inferred only from a run of strings at the same fret;
  - `cde-todo` (`song.js`) lists the chords the song uses that have no
    diagram.
- **Guitar Pro files**:
  - `gpfacts.Diagram` holds the file's chord diagrams: frets from the
    highest string, a base fret and barres. `Diagram.Spec()` writes them
    in this app's grammar.
  - `gpdrift.go` compares them with the text's.
  - The GP editor's palette (`gp-palette.js`) has a **chord-name tile**
    (A) and no diagram editor. Chords marked "show diagram" with no
    frets are hidden.
  - alphaTab's `ChordDiagrams` element is **hidden** in the score view
    (`gp-view.js`), the bar picker and the import.
  - alphaTab's `Chord` model is `name`, `firstFret`, `strings[]` (from
    the highest string, −1 muted), `barreFrets[]` and the
    `showDiagram/showFingering` flags. It has **no finger array**.
    ([model](https://alphatab.net/docs/reference/types/model/chord),
    [notation elements](https://alphatab.net/docs/reference/settings/notation/elements))
- **What a finder can build on**:
  - `chartalign.ParseSymbol`/`qualityIntervals` turn a chord symbol into
    its root, bass and pitch classes (triads, 6, 7, maj7, 9, sus, dim,
    aug, 5).
  - `chordsym` handles names, spelling and transposition.
  - `tuner.js` has a list of tunings as MIDI notes: guitar standard and
    alternates, ukulele GCEA, low G, baritone DGBE, banjo and others.
  - `internal/instruments` has `guitar` and `ukulele`.
  - The soundfont ([soundfont.md](soundfont.md)) could strum a voicing.
- **Not there**: any dictionary data, finger numbers, left-handed
  display, an instrument choice for diagrams, or a reverse lookup.

## 2. Data: generate the voicings, and check them against a curated set

| Source | What | Licence | Use here |
|---|---|---|---|
| **Generated** from intervals and the fretboard | any chord the name grammar can say, any tuning, any capo, any instrument | ours | **the dictionary itself** |
| [tombatossals/chords-db](https://github.com/tombatossals/chords-db) | guitar: 12 keys × 86 suffixes = 828 chords, 3,283 positions. Ukulele: 552 chords, 2,114 positions. Each position has `frets`, `fingers`, `baseFret`, `barres` and `midi`. | **MIT** | a **test oracle** (does the generator find these?), a **ranking hint** (a voicing in the curated set ranks higher), and **finger numbers** when the generator's own assignment is unsure. Keep its licence notice. Its keys are spelled C#/F# for guitar and Db/Gb for ukulele, so normalise through `chordsym`. |
| [ChordPro built-in configs](https://github.com/ChordPro/chordpro/tree/master/lib/ChordPro/res/config) (`guitar.json`, about 1,155 entries; `ukulele.json`; mandolin, charango, keyboard) | `{name, base, frets[], fingers[]}` | Artistic License 2.0 | a second oracle. It is usable with the notice, and we don't need to ship it. |
| [hyvyys/chord-fingering](https://github.com/hyvyys/chord-fingering) (npm) | a fingering generator for any tuning | **GPL-3.0** | avoid, because it is copyleft. Read its approach only. |
| [@tombatossals/react-chords](https://github.com/tombatossals/react-chords), [SVGuitar](https://github.com/omnibrain/svguitar), [VexChords](https://github.com/0xfe/vexchords) | SVG renderers | MIT | not needed, because `chorddiagram.RenderSVG` exists. They are useful as references for the finger and barre layout. |

Why generate:

- A curated set covers standard tunings and common qualities only. It
  has no capo-relative shapes, no open-D or DADGAD, nothing for a
  baritone ukulele and no slash chords beyond a few.
- A curated set also spells names its own way.
- The generator reads the chord through the same grammar as the chart
  (`chordsym`, `chartalign`), so every chord a song uses has voicings.

The curated set keeps the ranking honest: the usual shapes come first.

## 3. The finder

### 3.1 Input

- A chord symbol, giving its root, its pitch classes and an optional
  slash bass.
- An instrument and tuning: strings as MIDI notes, lowest first, as in
  `tuner.js`. A re-entrant flag for ukulele GCEA and banjo.
- A capo. The shapes are relative to it, and the fret numbers shown are
  counted from the capo.
- The number of frets to search: 12 for guitar diagrams (15 for a "up
  the neck" view), 12 for ukulele.

### 3.2 Enumerate

- For each window of 4 frets (5 with "stretch" on), starting at frets
  0–12, each string is either:
  - muted;
  - open, if its pitch class is in the chord;
  - fretted at a fret in the window whose pitch class is in the chord.
- Each string has one to three options, so a guitar window gives at most
  a few hundred combinations. All windows together take a millisecond or
  so, in Go or in JS.
- Keep a voicing when:
  - every **required** tone is in it: the root (unless a bass player has
    it; see the open questions), the 3rd or the sus note, the 7th and
    any written extension. The 5th is optional, as are the 9th of a 13
    and the root of a rootless jazz voicing in "jazz" mode.
  - the lowest **sounding** note is the root, or the slash bass for
    `C/G`. An inversion is kept with a penalty when no root position
    fits. On a re-entrant tuning (GCEA) the rule doesn't apply: there
    the bass isn't the lowest string.
  - at least 3 strings sound on guitar, and 3 on ukulele (4 preferred);
  - the **fingers** are 4 or fewer, after the barre.

### 3.3 Fingers and barres

- A barre sits at the lowest fretted fret. It needs every string from
  its first one to the highest to be at that fret or above, with none
  open and none muted inside it.
  - A partial barre on the top 2–4 strings is fine (`7:XX(3121)`).
- The remaining fretted notes are given fingers 1–4 in fret order,
  then string order.
- An index finger already used by the barre continues to 2–4.
- If no assignment fits, drop the voicing.
- Where chords-db has the same frets, prefer its fingers.

### 3.4 Score (lower is easier)

The score is a weighted sum:

- the span in frets;
- the fret position (low first, with a smaller weight for "up the neck"
  lists);
- the number of fingers;
- a barre: full barres (E and A shapes) cost less than odd partial ones;
- muted strings **inside** the voicing (`x3x2..`), which are hard to
  strum;
- missing the 5th (small) and doubling the 3rd (small);
- an inversion when the chart didn't ask for one;
- a **bonus** for open strings (beginner), for the root in the bass, for
  more sounding strings (strumming), and for a match in the curated set.

Ranking defaults:

- **Guitar, "beginner"** (the default): open and first-position shapes
  first, then barre E and A shapes, then everything else.
- **Guitar, "positions"**: grouped by **CAGED** shape. The shape is
  named from where the lowest root sits and from which open shape the
  pattern moves (C, A, G, E, D). The groups run up the neck, like
  Oolimo's and Guitar Pro's lists.
- **Guitar, "jazz"**: 4-note drop-2 and drop-3 shells on string sets
  6-4-3-2 and 5-4-3-2, rootless allowed.
- **Ukulele**: all four strings sounding, first position first, and no
  root-in-bass rule on GCEA. Baritone DGBE uses the guitar's top four
  strings, so its own rules apply, with the bass rule on.

### 3.5 Reverse lookup: name the chord from its frets

1. Turn the frets into pitch classes and the lowest sounding note.
2. Try each of the 12 roots with each quality the grammar knows.
3. Keep the names whose required tones are all present and that add
   nothing foreign. Rank them:
   - an exact match, fewest omitted tones;
   - root in the bass, otherwise a slash name;
   - the simplest name.
4. Return the first few ("C6 · Am7/C").

The editor's builder uses this to name the shape being drawn. A GP file's
unnamed diagram gets a name the same way. It is the same feature as
[jguitar's Chord Namer](https://jguitar.com/chordname),
[Oolimo's analyzer](https://www.oolimo.com/guitarchords/analyze) and
Guitar Pro's chord dialog.

### 3.6 Left-handed

- A person's setting mirrors every diagram horizontally: the highest
  string on the left.
- `RenderSVG` and the PDF take a `mirror` flag. The data doesn't change.
- The same setting can mirror the lesson fretboard and video
  ([lesson-page-review.md](lesson-page-review.md) §7).

## 4. Where it shows

- **The song page's chord strip**:
  - It shows the chords the song uses (`song.UsedChords`), each once in
    order of first use, as diagrams.
  - The diagrams are drawn at the **shapes the person plays**: the
    resolved key (written key + the place's key + tonight's) minus the
    capo. See key-and-transpose.md §3.3.
  - The person's instrument (guitar or ukulele) comes from their profile
    instruments, with a toggle on the strip.
  - What is drawn, in order:
    1. a voicing the person chose;
    2. the song's `chord:` / `chord[K]:` line for that key;
    3. the finder's first voicing.
  - It replaces "No chord diagrams for %s yet." The strip follows the
    Key and Capo controls live.
- **Tap a chord** to open a sheet with its voicings:
  - swipe through them, with the positions up the neck;
  - each one can be heard (soundfont strum);
  - "Use this one in this song" and "Use it everywhere" (see §5);
  - reverse lookup: "this shape is also Am7/C".
- **The editor**:
  - the Scores tab's builder gets "Suggest" (the finder's list for the
    name typed) and names a drawn shape;
  - `cde-todo` gets "Fill all with the first suggestion", which writes
    `chord:` lines.
- **The PDF**: the legend row prints the strip as the page shows it for
  the songbook's key, instrument and capo. A per-songbook PDF option
  could give "guitar", "ukulele" or "none".
- **Guitar Pro**:
  - the palette's chord tile can attach the finder's voicing
    (`strings`, `firstFret`, `barreFrets`; alphaTab keeps no fingers);
  - the score view can show alphaTab's `ChordDiagrams` when the file
    has them.

## 5. Who chooses a voicing: the shared song and the person

The song's text keeps **the song's own choice**, as `chord:` lines in its
written key, for one instrument (see the open questions). That fits the
model in [ownership-model.md](ownership-model.md):

- **A catalogue song**: diagrams are a fix like a wrong chord. They are
  applied at once, because they never change how the chart follows the
  recording, and they are revertable in the history.
- **A band's song or arrangement**: the band's choice ("we all play the
  F as `1:(133211)`"), edited by whoever edits the song.
- **A linked song**: changing its diagrams would make an arrangement,
  which is too heavy for a fingering. The choice goes on the **entry**
  instead: a songbook entry's voicings, which a teacher can set for a
  student.

**The person's choice** is never in the text:

- "For me, in this song" is per person, song, instrument and chord.
- "For me, everywhere" is per person, instrument and chord, at any key
  shape. For example, "my C is x32013".

This needs a table, `person_chord_voicings` (person, instrument/tuning,
chord, song or null, spec). It would be the first migration of this
work.

Order of precedence on the strip: the person's choice for this song →
the person's choice everywhere → the entry's → the song's `chord:` for
the key → the finder's.

## 6. The format: what a `chord:` line would need

- **Fingers**: the `songs` package's own `\gtab` already allows a finger
  string after the grid, as in `\gtab{A}{X02220:001230}`. Adopt it the
  same way.
  - `chord: A X02220:001230` and `chord: F 1:(133211):134211`.
  - The first `:` is the base fret only when it is a number, which keeps
    old specs valid.
- **Instrument**: today a `chord:` line is for whatever instrument has
  that many strings. A four-token spec is a ukulele only by guessing.
  Options:
  - `chord(ukulele): C 0003`, and `chord(ukulele)[G]:` for another key;
  - or a tuning token at the start of the spec (`GCEA/0003`).
  - The first reads better and leaves the grid grammar alone.
- **Tuning and capo**: none. Diagrams are shapes for the instrument's
  standard tuning, relative to `capo:`. An alternate tuning is the GP
  file's business, or a later `tuning:` header.

## 7. Competitors

- **Ultimate Guitar**
  ([tab tools](https://help.ultimate-guitar.com/en/articles/14880177-what-are-tab-tools-and-how-to-use-them)):
  - diagrams on chord tabs, transpose, a "Simplify" on official chords
    and a left-handed layout setting;
  - ukulele and capo as search filters;
  - it offers voicing variations per chord, but this note couldn't check
    the details.
- **Songsterr**
  ([example](https://www.songsterr.com/a/wsa/green-day-good-riddance-time-of-your-life-chords-s75)):
  - a chords page with the tuning, a "Capo OFF / Auto" control and
    compact fingering codes;
  - diagrams are thin.
- **Chordify**
  ([App Store](https://apps.apple.com/us/app/1073624757)):
  - animated guitar, ukulele and piano diagrams for detected chords;
  - capo and transpose are Premium.
- **Guitar Pro 8**
  ([chord diagrams](https://www.guitar-pro.com/docs/gp8/tools/chords-diagrams)):
  - its chord dialog names a chord from two notes and proposes barres;
  - it lists **alternative diagrams sorted by complexity**, follows the
    track's tuning and string count, and keeps a chord library per
    track.
  - This is the closest to the finder described here.
- **Uberchord** ([site](https://www.uberchord.com)): chord recognition
  from the microphone, and a chord finder in 19 tunings.
- **Oolimo**
  ([analyzer](https://www.oolimo.com/guitarchords/analyze)):
  - a chord finder and analyzer with "Possible Chord Names" and a tuning
    manager;
  - its reverse lookup is the reference.
- **jguitar** ([chord namer](https://jguitar.com/chordname)): a chord
  namer, search and calculator.
- **ukulele-chords.com** ([site](https://ukulele-chords.com)): a
  dictionary for GCEA, baritone DGBE, D tuning and slack-key, with a
  chord finder.

What none of them do is the strip at the **place's key and capo** in a
shared chart, with the band's or teacher's choice stored on the song or
the entry.

Research on scoring:

- d'Hooge et al., *Guitar Chord Diagram Suggestion for Western Popular
  Music*, SMC 2024 ([arXiv](https://arxiv.org/abs/2407.14260)): it
  suggests diagrams from the label and the previous diagram, which is
  voice leading between chords.
- Vélez Vásquez et al., ISMIR 2023
  ([paper](https://dare.uva.nl/id/6b0a80e9-7387-4cf8-8952-a28cef06a8ea)):
  a seven-criterion playability rubric. It is a good check for §3.4's
  weights.

## 8. Effort, in phases

1. **The finder** (Go, `internal/voicings`), about 3–4 days:
   - enumerate, fingers and barres, score, CAGED labels, reverse lookup;
   - guitar standard and ukulele GCEA;
   - tests against chords-db: the common shapes in the top 3, and every
     chords-db position found;
   - `GET /api/chords/voicings?chord=&instrument=&capo=` and
     `/api/chords/name?frets=`.
   - Go, so that the page, the PDF and the API share one
     implementation. The page fetches a key's strip on Key and Capo
     changes, which is small.
2. **The song page strip and the tap sheet**, about 2–3 days:
   - the instrument toggle, left-handed (a person setting), capo-aware;
   - after the player/scroller work, because it touches `song.js` and
     the controls of key-and-transpose.md §3.2.
3. **Choices**, about 2 days:
   - `person_chord_voicings` (a migration);
   - "use in this song" and "everywhere";
   - the song's `chord:` lines from the sheet (editors), and the entry's
     voicings for a teacher;
   - the PDF legend from the same strip.
4. **The format and the editor**, about 1–2 days:
   - fingers in Spec;
   - `chord(ukulele):`;
   - the builder's Suggest and naming, and "Fill all".
5. **Later**:
   - alternate tunings (tuner.js's list), baritone and low-G ukulele,
     banjo and mandolin;
   - the GP palette diagram;
   - voice-leading ranking (the next chord near the last);
   - hearing a voicing.

That is about 8–11 days for phases 1–4.

## 9. Open questions

- **The instrument**: from the person's profile instruments, from the
  song's GP tracks (a ukulele track), or a toggle remembered per
  person? Both instruments at once in the PDF?
- **A song's own diagrams**: one instrument (guitar by default, ukulele
  only via `chord(ukulele):`), or both?
- **The catalogue**: are diagrams applied at once for anyone (they don't
  touch the recording fit), or held for review like a newcomer's?
- **Root required?** With a bass player, a guitarist often drops the
  root. Should a band context switch on "rootless allowed"?
- **Simplify**: UG's "Simplify" turns extended chords into basic shapes
  (C9 → C). Is that a finder mode ("beginner: triads only"), shown as a
  hint on the chord ("C9 · play C")?
- **Capo suggestions**: for a key full of barres, suggest a capo that
  gives open shapes. For example, "capo 1, shapes in E" for F. This
  belongs to the Capo control of key-and-transpose.md.
- **Which tunings in phase 1**: guitar standard and ukulele GCEA only,
  or also low-G and baritone, which are cheap with a generator?
