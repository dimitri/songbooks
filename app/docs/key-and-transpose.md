# `key:`, `transpose:` and `capo:` in the song format

Status: **notes for a decision** (2026-11-24). Nothing is built from
this document yet.

The question (the user, 2026-11-24): should the song format have a
separate `key:` and `transpose:` header line, as the LaTeX `songs`
package and LilyPond do, to remove some confusion?

**The short answer: partly.** We should separate the ideas, and most of
the separation is already in the data. What we should not add is a
`transpose:` line in the shared song's text:

- The written key (`key:`) belongs to the song.
- The key a song is *played in* belongs to the place it is played:
  - a songbook entry (`semitones`), which also covers a band, a student
    and a group;
  - an event's program;
  - tonight's view.

  That is what LaTeX's `\transpose` and LilyPond's `\transpose` already
  are: a book's or a score's copy of the song shifted, with the music
  written once.

What is missing is mainly saying these things clearly. Concretely:

- `key:` gets one meaning.
- `capo:` gets real semantics.
- The `transpose:` header, which nothing reads, is retired.
- The page names the three things the same way everywhere.

## 1. What there is today

### The header lines

| Line | Documented as (`docs_song_format.html`) | What reads it |
|---|---|---|
| `key:` | "Key signature, e.g. `Bb`, `F#m`. The song's plain `chord:` diagrams are for this key." | See the list below this table. |
| `capo:` | "Capo fret, printed as an annotation." | The song page's header, the tuner (`songCapo`), the import key check (`songimport.CheckKey`: *sounding = key + capo*), LaTeX export (`\capo`), the importer's header merge. **Not** the player, the melody sheet, the catalogue's key rule or `KeyShift`. |
| `transpose:` | "Semitones to transpose for display — never changes the chord text itself." | **Nothing** except LaTeX import and export (`latexsong`). It is in `song.MetaKeys` and the format page, but the song page, the PDF, the player and the melody sheet never apply it. On staging, 0 of 258 songs have one. |

What reads `key:`:

- the song page header, `#key-original` (`play.html`);
- the transpose spelling table, `chordsym.TransposedSpellings`, which
  needs a key;
- the diagram sets: plain `chord:` lines are for `key:`, and
  `chord[K]:` lines are for other keys (`Song.DiagramsFor`);
- the melody sheet's key signature (`melody.KeyOf(key, transpose)`);
- the Jazz chart;
- the GP drift check against the file's key signature (`gpdrift.go`);
- the import key check (`CheckKey`);
- the catalogue's review rule (a key change waits for review);
- `song.KeyShift`, through `store.keepEntryKeys`;
- the name of a coach's key (`songKeyName`, which guesses from the
  chords when there is no header);
- LaTeX export.

Staging (demo data, 2026-11-24):

- 258 songs: 117 with `key:`, 7 with `capo:` (5 of them with both),
  0 with `transpose:`, 3 with `chord:` diagrams.
- 264 songbook entries, none with a key (`semitones ≠ 0`).

The legacy corpus in this repository:

- 8 `.tex` songs use `\transpose{n}` (Mercy, Trio), and 3 use `\capo`.
- 9 LilyPond files use `\transpose from to`.
- `latexsong.Parse` reads `\transpose{n}` into a `transpose:` header and
  deliberately leaves the chords alone. *Gone* is written in Bm,
  `\transpose{1}`, and is played in Cm.

### The keys a song is shown in

1. **The written key**: the chart's `key:`. For a catalogue song this is
   the recording's key. The catalogue refuses a chart transposed as a
   whole (`chartTransposed`, 422) and holds a key change for review.
2. **A place's key**: `songbook_entries.semitones` (a person's, a
   band's, a student's or a group's songbook) and `event_program.semitones`.
   It is relative to the written key. It opens the chart and the
   recording in that key (`data-key-offset`). It is shown as "Your key:
   A", "Chloé's key: A (+2)" or "in the key of 'Mercy': A · Original
   key". `?key=original` gets back to the written key. Set lists have no
   key of their own.
3. **Tonight's −/+** on the song page (`song.js`). It is view only and
   resets on reload. It moves the chords and **not** the recording, as
   "capo arithmetic". Its tooltip says "Play in another key: only
   changes what's on screen."
4. **The player's Key** (`audio-player.js`, ±7). It pitch-shifts the
   recording **and** clicks the chords along. It is kept per song in
   this browser (localStorage).
5. **The PDF**: a songbook's PDF prints each song in its entry's key
   (`transposedBody`, which uses `song.TransposeText`).
6. **The editor's Transpose** (Source tab). It rewrites the chords and
   `key:` for real, for everyone. It is off on catalogue songs.

### Where it confuses

- **Changing `key:` is read as moving the song.**
  - `song.KeyShift` treats a header that moved in the same mode as a
    transposition "whether the chart was transposed with it or the header
    alone was put right".
  - `keepEntryKeys` then moves every kept key back by as much.
  - So a label fix (the chart is in A but says `key: G`) turns a
    student's G (−2) into F (−4). The student's key and the recording's
    pitch shift both move, although no chord did.
  - The user's rule of 2026-11-24 is that fixing the key must not move
    the recording. `catalogue-news` is making a header-only key change a
    label fix. This document builds on that.
- **`transpose:` is documented and does nothing.**
  - Someone who knows the LaTeX or LilyPond habit writes
    `transpose: 2`, sees no change, and transposes the chart in place
    instead.
  - That is refused on a catalogue song. Elsewhere it moves every
    entry's key.
- **Four −/+ that look alike.** The page's −/+, the player's Key, the
  songbook's key and the editor's Transpose differ in what they move
  (the chords, the recording, a stored key, the text) and in how long
  the change lasts. The page's −/+ and the player's Key are both labelled
  "key".
- **What `capo:` means is not settled.**
  - The import check reads `key:` as the shape key, so the sounding key
    is key + capo.
  - The melody sheet reads `key:` as the sounding key, so its key
    signature is wrong under a capo: a chart in C shapes with capo 2
    and a recording in D gets a C signature on notes in D.
  - The catalogue's "the key is the recording's" doesn't say whether it
    means `key:` or `key:` + capo.
  - The player assumes the chords as written are the recording's pitch.

## 2. Prior art

| Tool | Written key | Shift at the point of use | Capo |
|---|---|---|---|
| LaTeX `songs` ([CTAN](https://ctan.org/pkg/songs), [manual](https://songs.sourceforge.net/songsdoc/songs.html), §7 "Automatic Transposition", §5.8 "Guitar Capos") | **None.** `\beginsong` has no key field. The spelling is guessed from the first chord, overridden by `\prefersharps` or `\preferflats`. | `\transpose{n}`: every later chord in the song, cumulative, reset at `\endsong`. It is written in the book's copy of the song. | `\capo{n}` only prints "capo n". With the `transposecapos` option it acts as `\transpose` (to print a guitar book for piano). |
| LilyPond ([key signature](https://lilypond.org/doc/v2.24/Documentation/notation/displaying-pitches), [`\transpose`](https://lilypond.org/doc/v2.24/Documentation/notation/changing-multiple-pitches#transpose), [`\transposition`](https://lilypond.org/doc/v2.24/Documentation/notation/displaying-pitches#instrument-transpositions)) | `\key d \major` sets the printed signature and never changes a pitch. | `\transpose from to { }` is a relative shift and **transposes the `\key` inside it too**. The spelling follows from→to. | No capo. `\transposition` declares a transposing instrument's sounding pitch (MIDI and cues, not print). |
| ChordPro ([key](https://www.chordpro.org/chordpro/directives-key/), [transpose](https://www.chordpro.org/chordpro/directives-transpose/), [capo](https://www.chordpro.org/chordpro/directives-capo/), [settings](https://www.chordpro.org/chordpro/chordpro-configuration-generic/)) | `{key: C}` is the key the song is written in. It can appear several times (modulations). | `{transpose: n}` applies from where it appears; at the top it applies to the whole song. A suffix sets the spelling (`2f`). It doesn't change a key directive written before it. The tool derives `key_actual` (after the shift) and `key_from` (before it). | `{capo: 2}` is metadata, and the key is the player's: `{key: C}` + capo 2 sounds in D. The derived `_key` is the sounding key. `settings.decapo` removes the capo by transposing. |
| OnSong ([metadata](https://onsongapp.com/docs/features/formats/onsong/metadata), [transposition](https://www.onsongapp.com/docs/transposition)) | `Key:` | `TransposedKey:` (`In:`) is "the desired key that the song should be played in". The slider is per song, and with "Separate Set/Song Styles" per set, leaving the master song unchanged. | `Capo:` is a fret count and can be negative. |
| iReal Pro ([soft](https://technimo.helpshift.com/hc/en/3-ireal-pro/faq/330-transposition-soft-1512081095/), [hard](https://technimo.helpshift.com/hc/en/3-ireal-pro/faq/389-transposition-hard-in-editor-1524602914/)) | The chart's key, marked "Default". | Soft transpose leaves the chart untouched and is remembered **per playlist**. Hard transpose in the editor is either "Set" (relabel only) or "Set AND Transpose" (rewrite). | No capo. A transposing instrument shows its own key. |
| Ultimate Guitar ([tab tools](https://help.ultimate-guitar.com/en/articles/14880177-what-are-tab-tools-and-how-to-use-them)) and Songsterr | Charts usually written in shapes with a "Capo n" note | Transpose is a view tool. | Songsterr has a "Capo OFF / Auto" control. |

What they agree on:

- The written key, the performance key and the capo are three different
  things.
- The performance key lives **next to** the music, not inside it:
  - in the book's copy of the song (LaTeX, LilyPond);
  - in the set or playlist (OnSong, iReal);
  - or as a view (UG).
- A **relabel and a rewrite are distinct operations**. iReal Pro names
  them "Set" and "Set AND Transpose".
- A capo leaves the written shapes alone. The sounding key is derived
  from it (ChordPro's `_key`).

In this app's terms:

- LaTeX's book copy and iReal's playlist are the **songbook entry**.
- ChordPro's `{transpose}` at the top of a file is the same idea, but it
  sits in the file because ChordPro has no database.
- An in-file shift fits a one-person file of songs. In a shared song,
  where every place keeps its own key, it would be a fifth layer.

## 3. Proposal

### 3.1 `key:` is the written key, and only that

- `key:` names the key **the chords are written in**. It is a claim
  about the text, checkable against the chords (`songimport.ChordKey`),
  and nothing more.
- For a catalogue song, the written key is the recording's key, with
  the capo applied (§3.3).
- **Changing `key:` alone is always a relabel.** It never moves a kept
  key, the recording's pitch shift or a diagram set. This makes
  `catalogue-news`'s rule general.
  - Only the chords moving together is a transposition. That is
    `KeyShift`'s second branch: the editor's Transpose, or a chart
    rewritten by hand.
  - The editor's Transpose rewrites the chords and `key:` together, as
    it does today ("Set AND Transpose").
  - A wrong label is fixed in the header ("Set"). The editor can offer
    "Your chords read as A: write `key: A`?" when the header and
    `ChordKey` disagree with high confidence.
- With no `key:` line, the key is the one guessed from the chords, as
  `songKeyName` already does. The page shows it as a guess ("key A?").

### 3.2 The key a song is played in stays in the places, under one name

No `transpose:` line in the song's text. The shift belongs to whoever
plays the song:

| Whose | Where it is kept | Moves the recording? | Lasts |
|---|---|---|---|
| A songbook's (a band's, a person's own, a student's, a group's) | `songbook_entries.semitones` | yes | stored |
| An event program's | `event_program.semitones` | yes | stored |
| A set list's | its songbook entry's (no key of its own) | yes | stored |
| A person's for this song, from nowhere in particular | their own songbook's entry (`MySongbookKey`) | yes | stored |
| Tonight's | the page's Key, in this browser | yes | this browser |

The plan:

- **One word everywhere: "Key"**, always relative to the written key,
  always shown as both "A (+2)" and "written in G".
- **Merge the page's −/+ into the player's Key.** It becomes one
  control that moves the chords and the recording together.
  - What the −/+ does today (chords only, recording untouched) is a
    capo, so it becomes the Capo control (§3.3), labelled as such.
  - That removes the two-controls-named-key problem.
- The PDF, the melody sheet (its `?transpose=N`), the GP score view and
  the jazz chart all read **the same resolved key** for the page:
  written key + the place's key + tonight's.
  - Today the page's `StudentKey` resolution (`handlers_pages.go`) is
    the single source on the server side.
  - It should become one function that the PDF and the API share as
    well.

**If we did add `transpose:` to the file**, it would mean "this song's
default shift", above the written key and below every place's key.
That rules it out:

- A catalogue song can't have one (its key is the recording's).
- A band's or student's key would be relative to the written key or to
  the shifted key, which is ambiguous either way.
- `keepEntryKeys` would have to treat a change to it as a move.
- Two people's arrangements would differ only by a number in their text,
  and the ownership model already says "a key never makes an
  arrangement".

### 3.3 `capo:` gets real semantics: the written chords are shapes

Following ChordPro and the import check that already exists:

- **`capo: n` means the chords are written as shapes for a capo on fret
  n. The song sounds at `key:` + n.** That is the *sounding key*.
- The **recording's key is the sounding key.** The catalogue's rule
  becomes "the sounding key is the recording's", so a UG-style chart in
  shapes with "capo 2" is a valid catalogue chart.
- What reads the sounding key:
  - the melody sheet's key signature and notes;
  - the player (its Key 0 plays the recording as is, and the chords
    stay as written);
  - the import's key check (already does);
  - the GP drift check (a GP file's key signature is the sounding one,
    and its track capo is in `gpfacts`);
  - the coach's range check;
  - the PDF's "sounds in D" note.
- What reads the written key: diagrams (they are shapes) and spelling.
- **A person's capo is a view.** The page's Capo control (today's −/+)
  shows the chords as shapes for a capo the person chose, while the
  recording and every key stay where they are. It is shown as "Capo 2 ·
  shapes in C · sounds in D".
  - It is kept per song in this browser, like the player's settings.
  - A songbook entry could keep one too ("Chloé plays it capo 3"); see
    the open questions.
- "Remove the capo" (ChordPro's `decapo`) is the editor's Transpose by
  +n with `capo:` removed, which makes concert-pitch chords.

### 3.4 Each place

| Place | Reads |
|---|---|
| Editor (header form, Source) | `key:` with a "chords read as …" hint; `capo:`. Transpose = "Set AND Transpose" (chords + key). Changing the key field alone = a relabel, and the hint says so. |
| Song page header | "Key **A** (+2, Chloé's key) · written in G" or "Key G"; "capo 2 · sounds in A" when `capo:` is set. |
| Song page controls | **Key** (the player's, moves chords and recording; the place's key is its starting point) and **Capo** (shapes only). |
| Songbook entry / student / group / event / band | `semitones`, relative to the written key, as today. `keepEntryKeys` runs only on a real transposition. |
| Set list | its entry's key (no change) |
| PDF | written key + the entry's key, then the capo printed ("capo 2"); optionally decapo'd for a piano book. |
| GP file | its key signature is compared with the **sounding** key; its tracks' capo is compared with `capo:`. |
| Recording pitch shift | written key + capo = the recording's key. The Key control shifts from there. |
| Melody sheet | key signature from the sounding key, shifted by the resolved Key |
| Chord diagrams | for the shapes (written key + Key − Capo); see `chord-dictionary.md` |
| LaTeX / ChordPro export | `\capo` / `{capo:}` as written; a songbook entry's key as `\transpose{n}` / `{transpose: n}` in the exported book (the book's copy), with the chords left as written. |
| LaTeX import | `\transpose{n}` becomes the **songbook entry's** key when importing into a songbook. If there is no songbook, the editor offers to transpose the text. It never becomes a header. |

### 3.5 Existing songs

- `transpose:` headers: none on staging. Production needs checking
  (a count query); the legacy corpus has 8 `.tex` songs with
  `\transpose`.
  - A one-off script moves each one to the owner's songbook entry
    (`semitones`) and drops the line.
  - The parser keeps reading the line (round trip), and the format page
    calls it "LaTeX import only, see the songbook's key".
- `capo:` songs (7 on staging): check each with `CheckKey`.
  - If key + capo matches the recording's analysed key, keep it as is
    (shapes).
  - If `key:` alone matches (someone wrote the sounding key with a capo
    note), relabel `key:` down by the capo.
  - A staff list for the doubtful ones.
- `key:` that disagrees with its chords (`ChordKey` with confidence): a
  staff list. Fixing is a relabel, which after step 1 below moves
  nothing.
- `chord:` diagrams: unchanged. They are shapes, already per key.

## 4. What it removes

- A key fix no longer moves students' keys or the recording.
- No line in the format that does nothing.
- One "Key" control with one meaning (moves the music), plus a "Capo"
  control that says what it does, instead of two −/+ both called key.
- The catalogue's rule can be checked under a capo.
- The melody sheet has the right key signature under a capo.
- The LaTeX/LilyPond habit maps to something real: the book's
  `\transpose` is the songbook's key.

## 5. Risks

- **Retraining**: people who used the page's −/+ as "play it in A
  tonight" without the recording now get the Capo label for that. The
  recording moving with Key is what they usually want, but some will
  look for "chords only". The Capo control covers that, but its name is
  guitar-centric. A singer's "chords only, the recording stays" would be
  unusual anyway.
- **Capo semantics change some songs' checks.** A chart that wrote the
  sounding key in `key:` and a capo note would be flagged. That is the
  migration's staff list.
- **Merging −/+ into Key touches `song.js`, `audio-player.js` and
  `mixer.js`**, which the player and scroller work owns now. Wait for it
  (see the public pages freeze).
- **Spelling**: the resolved key is the place's. Two places in
  enharmonic keys (F# and Gb) keep spelling by `flats:` and the key, as
  today.
- **The public format page is frozen**: its `transpose:` row stays
  wrong until the freeze is lifted. It should be the first change after.

## 6. Plan, in order

1. **Relabel is not a move** (`catalogue-news`, in progress):
   `KeyShift` counts only the chords. A header-only change returns 0,
   and its doc comment and test say so. The editor shows "key relabelled
   (no chord moved)" in the save summary.
2. **One resolved key on the server**: a function (person, song, place
   in the query) that returns written key + place key, used by the song
   page, the PDF, the melody sheet's default `transpose` and the API.
   No behaviour change; it consolidates what is already there.
3. **Capo semantics**:
   - a `song.SoundingKey(sg)` (key + capo);
   - the melody sheet, the GP drift check and the catalogue's rule read
     it;
   - `CheckKey` already does;
   - the header shows "capo 2 · sounds in D";
   - tests for a capo chart in the catalogue.
4. **Migration report** (read-only): `transpose:` lines, capo charts
   checked against the recording, `key:` against the chords, on staging
   and then production. A staff list.
5. **Retire `transpose:`**:
   - the script that moves lines to entry keys;
   - LaTeX import writes the entry's key instead;
   - LaTeX/ChordPro export writes `\transpose` from the entry's key;
   - `MetaKeys` keeps the word for reading.
6. **The page's controls** (after the player/scroller work):
   - the −/+ becomes Capo (chords as shapes, the recording stays) with
     its own label;
   - the player's Key is the single key control, starting at the
     place's key;
   - the header line names written key, key and capo.
7. **The format page** (after the freeze):
   - `key:` "the key the chords are written in";
   - `capo:` "the chords are shapes for this capo; the song sounds at
     key + capo";
   - `transpose:` "LaTeX import only";
   - a short "Keys" section: written key, a songbook's key, tonight's
     Key, Capo.
8. **Optional**: a capo per songbook entry (§7).

## 7. Open questions

- **Capo per place?**
  - A student's or band's entry could keep a capo next to the key
    ("Chloé: key A, capo 2, shapes in G"). It is a new column
    (`songbook_entries.capo`).
  - Or it could stay a browser setting. A coach giving a key to a
    beginner guitarist would want it stored.
- **A modulating song**: ChordPro allows several `{key:}`. Today
  `key:` is one line, and a modulation is just the chords. Is a
  per-section key worth it for the melody sheet's signature and the
  diagrams (`key:` inside a section)?
- **Tonight's key**: stored per browser (as now) or per person on the
  server, so the phone and the laptop agree?
- **A person's own song without a recording**: its written key can be
  anything, and the editor's Transpose stays on. Should a person's song
  *with* an uploaded MP3 also refuse a whole-chart transpose, as the
  catalogue does, because the MP3 has a key too?
- **What the PDF prints for a capo chart in a piano book**: a
  `decapo` option per songbook PDF?
