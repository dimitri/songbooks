# Song editor — review and plan

Status: **phase 0 and phase 1 built** on branch `gp-editor`. The
rest is scoped and not built yet.

Guitar Pro is the reference for how editing notation should feel. The
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

**New voices:** "New voice" creates a `score[Guitar]` or `score[Bass]`
voice with one rest bar per measure in the section, in standard tuning.

**LilyPond voices:** a voice written in the LilyPond subset is converted
to a Riff (`POST /api/score/riff`) on first edit. A notice says it will
be saved as tab data from then on. Read-only viewing doesn't convert it.

**What the subset leaves out for now:** multiple voices per staff,
per-bar time signatures, tempo changes, vibrato, harmonics, dead or ghost
notes, let-ring, dynamics, a drum editor. These come in phase 4.

## 5. Phase 2 — the Guitar Pro round trip

The goal: "this came from a GP file — download, edit in Guitar Pro,
upload again" must be safe and quick.

1. **Download what you'll edit.**
   - "Download .gp" offers the stored original.
   - "Export current .gp" exports what the song holds now: load the stored
     original with alphaTab, replace the edited tracks' bars from the
     Riffs, then `alphaTab.exporter.Gp7Exporter`. Rebuild from the Riffs
     alone when there is no original. This way edits made in the web
     editor are never lost by a round trip.
   - Section names go in as GP markers, and the chords as chord names.
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
4. **Apply in one request, one transaction, one revision.** The new GP
   file is kept in a history (`gp_files` gains a version), and the old
   one stays downloadable. A same-sha upload says "nothing changed".
5. **Audio stays aligned:** recompute sync points from the stored
   mapping, and drop section markers that no longer land on a bar.
6. **Forks copy the GP file and the audio rows.**
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
- a drum editor (GP drum-map keys).

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
