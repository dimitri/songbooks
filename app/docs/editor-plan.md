# Song editor — review and plan

Status: **phase 0 and phase 1 built**, and part of phase 2 (GP file
history, export with web edits, forks keep their media) on branch
`gp-editor`. See §9 for where that leaves us against the goal.

**The goal is not a web Guitar Pro.** It is for a band to take notes and
share them easily: the changes, a riff, a bass line, a groove, a fix to a
transcription. Guitar Pro stays the tool for real transcription work, and
the round trip with it has to be safe. Guitar Pro is still the reference
for how editing notation should feel. The
`.song` text stays the single source of truth. It is still the file we
store, diff, revise and export, and every editor below reads and writes
it.

## 1. Where we are

The editor drawer has five tabs.

| Tab | What it really does |
|---|---|
| Chart | A chord-grid editor. Desktop shows one section at a time with a chord toolbar; mobile shows a list of section cards. |
| Scores | Only a chord-diagram builder for `chord:` lines. There is no score editor. |
| Source | The raw `.song` text with a live preview. |
| Import | Paste text, iReal or ChordPro, then review and append. |
| GP Import | A wizard: map Guitar Pro bars to sections, pull one track's notation and the backing track. |

**What works well:**
- the live preview;
- the iReal per-section review in Import;
- the GP Import alignment view;
- one Save/Discard pair for all tabs.

**What doesn't:**
- **A GP-imported score can't be edited.** It is stored as one long JSON
  line per section and track. The note's MIDI number, string and fret
  must be kept consistent by hand. Nothing converts it back to readable
  text, and the LilyPond subset can't say which string a note is on.
- **The chord editor isn't a typing tool on desktop.** There are no arrow
  keys, Tab or Enter. You can't type `m7`, a flat, `#` or `/` from the
  keyboard. Chords the palette doesn't offer (7#9, 7sus4, maj7#11, N.C.)
  need Source.
- **Edits get lost.** About fifteen edit actions don't mark the song
  unsaved or record an undo step. Escape closes the whole drawer even when
  it was meant to close a menu. Saving from Chart deletes every `chord:`
  diagram line (`jsonSong` has no field for them).
- **The GP round trip is fragile.** Re-importing forgets the previous
  alignment and re-ticks sections you unticked. It overwrites the header
  by default and writes N+2 separate revisions with no transaction. Forks
  lose the GP file.

The three detailed reviews behind this summary are summarised in §7.

## 2. Principles

1. **One model per kind of content.**
   - Chords and sections live in `grid:` and the section blocks.
   - Notation lives in `score[Label]:` voices, edited as a `tab.Riff`.
   - Lyrics live in the lyric lines.
   Each editor edits one of these and never re-derives another.
2. **The Riff is the notation editing model.** Every renderer already
   reads it: the chart rows, the Mixer, PDF, pickup detection and drum
   maps. alphaTab is only the view and hit-test layer. We edit the Riff,
   re-emit alphaTeX and re-render.
3. **Guitar Pro files are the interchange format, not the storage
   format.** We keep the uploaded original. We can export the current
   state back to `.gp`, and re-importing is a merge with a preview, never
   a blind overwrite.
4. **Every edit is undoable, marks the song unsaved, and saves as one
   revision.**

## 3. Phase 0 — data safety (small fixes, ship first)

- [x] Chart save keeps `chord:` diagram lines: add `chords` to the JSON
  wire shape.
- [x] Escape closes the open menu, popover or tab-editor selection
  before it closes the drawer.
- [x] Every Chart mutation calls `pushUndo()` and `markDirty()`: reorder,
  duplicate, repeat marker, rename, notes, endings, `%`, split, duration,
  bars-per-line. Deleting a section asks first.
- [x] Changing a bar's split keeps each chord on its beat.
- [x] GP Import keeps unticked sections unticked across re-renders, and
  a re-import no longer ticks header values the song already has.
- [x] `score[x]: { c'4 d' }` is LilyPond, not JSON. Only treat `{` as JSON
  when it is valid JSON.
- [x] A Riff with `Drums: true` and no tuning decodes (GP drum import
  itself is still phase 2).
- [x] A beat with several notes renders its hammer-on, bend and palm mute
  on every note.
- [x] Section picker: one preset list in song order, an inline custom
  name, "play again" for labels the song already has, and the new label
  is focused on desktop too.

## 4. Phase 1 — Guitar Pro–like tab editor (this branch)

This lives in the **Scores** tab and edits one section's voice at a time.
It writes into the same in-memory song (`structSections[i].scores`) that
the drawer's Save, Discard and Undo already handle.

**Layout:**
- a section picker and a voice (track) picker;
- the rendered tab for the section, with a cursor box on the current
  beat and string, Guitar Pro style;
- a toolbar showing the current beat's duration and effects, with the
  same actions as the keys below;
- a bar fullness indicator: "3 / 4 beats", red when over, amber when
  under.

**Keys (Guitar Pro defaults where they exist):**

| Key | Action |
|---|---|
| ← → | previous / next beat (moving past the last beat appends a rest beat) |
| ↑ ↓ | string up / down |
| Ctrl/⌘ ← → | previous / next bar |
| 0–9 | fret on the current string; a second digit within 0.6 s makes 10–24 |
| Backspace / Delete | remove the note on the current string; on an empty beat, remove the beat |
| Insert, or I | insert a rest beat before the cursor |
| + / − | shorter / longer duration (Guitar Pro's numeric-pad keys) |
| . | dotted on/off |
| R | make the beat a rest |
| T | triplet on/off |
| L | tie the note to the previous one |
| H | hammer-on / pull-off |
| S | slide into the note from the previous fret |
| B | bend: off, ½ tone, full tone |
| P | palm mute |
| Ctrl/⌘ C / V | copy / paste the current bar |
| Ctrl/⌘ Z / ⇧Z | undo / redo (shared with the Chart editor) |

**New bars:** "+ bar" appends a whole-bar rest. A bar can be deleted or
duplicated from the toolbar.

**New voices:** "New track" creates a guitar, bass or drum voice with one
rest bar per measure in the section, or a one-bar drum pattern.

**Bass:** an F-clef staff over the tab, in the chart and in the editor,
written an octave above where it sounds (the usual bass convention).
Guitar stays tab only. In the editor, click the staff to enter notes by
name (A–G, then ↑ ↓ by semitone; the editor picks the string and fret
nearest the hand), or the tab to type frets.

**Drums:** a percussion staff with a piece cursor (↑ ↓, or click). Keys
toggle a piece on the beat: K kick, S snare, H hi-hat, O open hi-hat,
C crash, Y ride, X side stick, F hi-hat pedal, 1–4 toms; Space toggles
the piece under the cursor.

**Patterns:** "Repeats through the section" (`"Pattern": true` in the
voice) marks a short groove or riff written once. `tab.LoadScores`
repeats it to the section's length. Tab rows draw the repeats as "%"
bars; the Drummer view and the Mixer write and play them in full. A
section with a score and no chord bars (an intro riff, a groove written
on its own) gets its bars from the score.

**LilyPond voices:** a voice written in the LilyPond subset is converted
to a Riff (`POST /api/score/riff`) on first edit. A notice says it will
be saved as tab data from then on. Read-only viewing doesn't convert it.

**What the subset leaves out:** multiple voices per staff, per-bar time
signatures, tempo changes, vibrato, harmonics, dead or ghost notes,
let-ring, dynamics, drum accents and flams. Most of it belongs in Guitar
Pro, not here (see §9).

## 5. Phase 2 — the Guitar Pro round trip

The goal: "this came from a GP file — download, edit in Guitar Pro,
upload again" must be safe and quick.

1. **Download what you'll edit.**
   - [x] "Original .gp" downloads the stored original.
   - [x] "Export as Guitar Pro" (`gp-export.js`) writes what the song
     holds now: every track over the whole song from the Mixer's alphaTeX,
     sections as markers, chords as chord names, tempo and swing, plus the
     original file's backing track and sync points when the bar counts
     still match. Edits made on the web are not lost by a round trip.
   - [ ] Patch the original instead of rebuilding, to keep the tracks and
     Guitar Pro effects the import never brought in.
2. **Remember the mapping.** Store the GP bar range and lead-in that fed
   each section and track, plus the source file sha256, in a
   `gp_mapping` table. On re-upload, pre-fill the alignment from it.
   Match sections by GP marker name first and bar position second.
3. **Preview as a diff before applying:**
   - per section and track: unchanged / changed (bar count, notes
     changed) / added / removed;
   - header changes are shown but **not ticked by default**;
   - chords from GP diagrams are an opt-in grid diff;
   - a hand-written (LilyPond) voice is never replaced without an
     explicit tick.
4. **Apply in one request, one transaction, one revision.**
   - [x] A new GP file keeps the one it replaces
     (`song_gp_source_file_version`), listed and downloadable on the
     song's History page; uploading the same file again changes nothing.
   - [ ] The import itself still writes one revision per section.
5. **Audio stays aligned:** recompute sync points from the stored
   mapping, and drop section markers that no longer land on a bar.
6. [x] **Forks copy the GP file (and its earlier versions), the
   recordings, their markers and bar sync points.**
7. **Robustness:** key voices on a stable GP track id (stored next to the
   label) rather than its display name; read every staff and voice;
   import drum tracks.

## 6. Phase 3 — the chord-sequence editor

The Chart tab becomes a fast, keyboard-first iReal-style editor.

- **A typed chord field.** Type `Bbm7/F` and press Enter. A parser with
  autocomplete accepts anything `chordsym` accepts, plus N.C. and `%`.
  Space moves one beat, Tab goes to the next bar, arrows move freely,
  Backspace clears.
- **Beat-based bars.** Typing two chords in a 4/4 bar splits it 2+2.
  Changing the split keeps every chord on its beat. Durations are shown
  in beats, not slots.
- **One editor at every width:** drag or keyboard reorder, duplicate,
  rename and delete in the section list on desktop too.
- **Section picker:**
  - presets in song order: Intro, Verse, Pre-Chorus, Chorus,
    Post-Chorus, Bridge, Solo, Interlude, Break, Outro, Coda, Tag;
  - an inline custom name, plus the labels already in the song;
  - picking a label that already exists offers "repeat Chorus" (a bare
    reference with a ×N stepper) or "new Chorus content";
  - the list shows what the chart will show: "Verse 2", rehearsal
    letters, references, ×N;
  - one preset list shared by Chart and Source (today there are two,
    with different entries).
- **Controls for what Source still owns today:** per-section time,
  repeat count `|:4`, endings as selectable bars, two-bar `%`.
- **A lighter preview:** update the chart locally, or post only the
  edited section, instead of the whole song on every tap.

## 7. Phase 4 — text sources and the rest

**Source tab:**
- **Lossless round trip.** Chart and score saves patch the raw text
  instead of re-serialising it. That keeps `#` comments, blank lines
  between stanzas, unknown header keys and the field order.
- **Errors where they happen.** Validate on save and in the preview, and
  show score and parse errors at their line instead of logging them on
  the server.
- **Readable GP notation.** Riff voices can be shown folded, and the
  LilyPond subset learns string numbers (`c4\5`) so a Riff can be written
  as text without losing fingering. Then GP-imported voices can be stored
  as text when they fit the subset.
- **Fix the lyric/header ambiguities:**
  - `[Am]` alone on a line is a section header;
  - a lyric line starting with `time:` or `grid:` is misread.
- **Document** comments, `gp_source`, and what each save path keeps.

**LilyPond:**
- `score[]` voices in the LilyPond PDF export, or say plainly that it is
  chords and lyrics only.
- `~` sets a real tie; the time signature comes from the song, not a
  hard-coded 4/4.

**Import tab:** a "replace sections" mode with a diff, besides "append".

**Notation model, toward Guitar Pro:**
- per-note techniques on chords (today only the tie survives on a beat
  with several notes);
- emit `BendRelease`;
- a time signature per bar;
- several voices;
- double dots;
- [x] a drum editor.

## 8. Review notes (condensed)

**Chart editor:**
- Desktop and mobile are two different editors, chosen by width.
- `setBarSplit` packs chords to the front, so they move off their beats.
- "1 beat" means one slot.
- The new section label is never focused on desktop and stays "New".
- `structure.js` is dead code.
- Genre saves on its own.

**Scores:**
- LilyPond bars are inferred at 4/4.
- `~` is dropped.
- Beats with several notes lose hammer-on, bend, palm mute and slide
  when rendered.
- GP import reads only `staves[0]` and `voices[0]`.

**GP round trip:**
- The "use the file's markers" option is off by one.
- Repeats are expanded in the riff but attached by measure index.
- The `gp_source` meta PUT result isn't checked.
- After an import, a later Source save hits 409 until the page reloads.

**Text:**
- A body starting with `{` is always read as JSON.
- Raw save doesn't validate.
- The client only ever says "save failed".

## 9. Review against the goal — what's missing

What a band can do now: write or fix a guitar, bass or drum part in a
section from the browser, keep a groove as a one-bar pattern, take a GP
file into Guitar Pro and back without losing web fixes or earlier files,
and undo or revert any of it from History.

What's missing, in the order it would help a band most:

1. **Hearing it.** The Scores tab can't play what you wrote. alphaTab's
   player (already used by the Mixer) should play the section, or the
   pattern on loop, at the song's tempo.
2. [x] **Phone and tablet entry.** A keypad under the score (on by
   default on touch screens, "Keypad" elsewhere): frets, note names on a
   staff, the moves and delete; the toolbar holds the rest. A phone-width
   editor draws two bars per row. Still keyboard-first on a computer.
3. **Notes on the music itself.** A short text note on a bar ("stop on
   4", "bass alone here") drawn above the staff, for the things a band
   actually writes down. Section notes exist; bar notes don't.
4. [x] **Showing who changed what.** History describes each edit in the
   song's terms (`internal/songdiff`: header values, sections added or
   removed, chords per bar, lyric line counts, score tracks per bar),
   with the text diff one click away.
5. **The rest of the GP round trip** (phase 2): remember the bar mapping
   so a re-import doesn't need re-aligning, show a diff before applying,
   one revision per import, and import drum tracks.
6. **Typing chords** (phase 3) on desktop: `Bbm7/F` + Enter.
7. **Small gaps in what was built:**
   - [x] tab, staff or both is a personal choice per instrument (reading
     tab or a staff is training and habit, not part of the song): kept on
     the person (`people.notation_prefs`, default guitar tab and bass
     both) and changed from the chart, the Scores tab or a Mixer lane;
   - [x] ride bell, cowbell and the low floor tom in the drum editor;
   - [x] the Drummer view sizes each bar by how busy it is, puts 1, 2, 4
     or 8 written bars on a line, numbers bars from where the song is,
     and drops alphaTab's stray "f" and tagline;
   - [ ] the chord names in an export sit on the first track only.

Deliberately not planned: several voices per staff, tempo and meter
automation, the full Guitar Pro effect set, print-quality engraving. For
those, export to Guitar Pro, edit there, and import again.
