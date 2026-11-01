# v2 — strict one-page performance sheets

A parallel pipeline. Nothing in `Mercy/`, `MySongs/`, `Trio/` or `Other Lies/` is
touched; those keep building exactly as before.

## Why

The old books are a flowing, multi-column *book*, which is the wrong shape for
reading one song off a stand with both hands occupied. `\sclearpage` clears the
column, not the page, so Mercy's A4 book was 39 pages for 43 songs with badly
unbalanced columns. Underneath that, a song's chords lived in two places — a
LilyPond `.ly` grid and inline `\[Chord]` markers — with two independent
transposition mechanisms kept in agreement by hand, and key/tempo existed
nowhere machine-readable (the hand-typed titlepage tables had already drifted).

## What it does

One `.song` file per song is the single source of truth: metadata, chord grid
and chorded lyrics together, one key, one transposition. Typst renders it to
exactly one page per target, choosing the largest type size that fits.

    make convert BOOK=Mercy      # .tex + .ly  ->  .song
    make BOOK=Mercy TARGET=a4    # build + assert one page per sheet
    make sheets BOOK=Mercy TARGET=tablet13 THEME=dark

Targets: `a4`, `tablet13`, `tablet11`. Themes: `light`, `dark`.

## The layout rules, and where they come from

* **Page**: A4 content block of 170 × 257 mm, i.e. 20 mm margins — MOLA's
  published image area. That geometry renders at ~102 % of print size on a 13"
  iPad once the reader crops margins, against 88.5 % for a full-bleed A4.
  The tablet targets are native-sized pages, so a point on the page is a point
  on the screen.
* **Type floor is 14 pt.** The arc-minute, proportional and x-height methods
  all put the minimum for reading at 70–90 cm at roughly 14 pt, with 17–19 pt
  as the comfortable target. The old books ran at ~10 pt.
* **4 measures per grid line, always.** A 32-bar AABA form then falls out as 8
  lines with each section exactly 2 lines; a 12-bar blues as 3 lines.
* **Chord symbols** follow Brandt & Roemer (1976) — the actual ancestor of Real
  Book house style: capital roots, quality text smaller and raised, accidental
  before the numeral, real ♭/♯ glyphs rather than `b`/`#`.
* **Chordless lines reserve no chord band.** That reserved band is what doubled
  the leading in the old `songs` output and drove the sprawl.

## Fitting a song on one page, in priority order

1. collapse repeated sections — a repeated chorus becomes a labelled marker,
   not a second copy of the text (costs no legibility at all)
2. two columns, with a section never split across the break
3. only then reduce type, and never below the floor

`compile-sheet.sh` verifies the real page count and lowers the type ceiling on
overflow, because Typst's `measure()` cannot see column balancing and the
estimate is necessarily approximate. A song that still will not fit at the
floor is reported — that is an editorial signal about the song, not a layout
failure to paper over.

## Layout

```
Makefile            entry point; see `make help`
mk/                 config.mk, scores.mk, sheets.mk, books.mk
lib/*.typ           the renderer: chord parsing, staff, lyrics, page template
render.typ          per-sheet entry point
tools/
  convert.py        one-shot import from the legacy LaTeX + LilyPond sources
  extract-score.py  pulls notation out of a .ly and renders it to SVG
  compile-sheet.sh  compiles one sheet and verifies it is one page
  check.sh          regression gate over a built target
  legacy/           superseded scripts, kept for reference
<Book>/
  songs/*.song      the source of truth: one file per song
  scores/*.svg      pre-rendered notation fragments (committed)
  known-overflow.txt  songs that cannot fit; the regression baseline
  book.typ          optional songbook assembly
build/              output, gitignored
```

## Usage

```
make                              build + verify Mercy on A4
make BOOK=OtherLies TARGET=tablet13 THEME=dark
make everything                   every book x every target
make book BOOK=OtherLies          assemble a songbook PDF
make scores                       re-render notation fragments (needs LilyPond)
make import BOOK=Mercy            re-import from the legacy sources
```

`SKIP_SCORES=1` skips the LilyPond rules entirely; the SVGs are committed, so a
machine without LilyPond can still build every sheet. CI uses this.

`make import` is a one-shot: once a song is imported the `.song` file is the
source of truth, and re-running overwrites hand edits.

## Continuous builds

`.github/workflows/songbooks.yml` builds every book and target on each push,
asserts the one-page invariant, uploads the PDFs as an artifact, and keeps a
rolling `latest` release so there is always a current set at a stable URL.
Typst is pinned: it is pre-1.0 and ships breaking changes in feature releases,
and a silent layout shift would be invisible in CI.

The check step fails on **regressions**, not on the known-overflow baseline in
each book's `known-overflow.txt`. A song that starts fitting again is also
reported, so the list cannot rot.

## Status

| book | target | one page |
|---|---|---|
| Mercy (43 songs) | a4 | 41 / 43 |
| Mercy | tablet13 | 41 / 43 |
| Mercy | tablet11 | 30 / 43 |
| Other Lies (30 songs) | a4 | 29 / 30 |

Full Mercy build, all 43 sheets: ~3.7 s wall clock.

## Open items

* 7 Mercy songs where the `.tex` and `.ly` transposition were never
  cross-checked; `make convert` lists them. Resolve each by hand.
* 4 Mercy fragments carry real notation (`TabStaff`/staff) rather than a chord
  grid and are not yet rendered: `always-free/verse.ly`, `bad-spell/intro.ly`,
  `mercy/mercy.ly`, `whiskey-beer-and-wine/intro.ly`. Plan is
  `lilypond -dcrop -dbackend=svg` once, SVG committed, included with `image()`.
  Note such an image cannot shrink with the fit loop, so it needs scaling in
  the same pass.
* Chord diagrams are not implemented. They are off by default by design: at a
  performance-legible size a 10-chord song costs 4–8 lyric lines of space.
* Lyrics are set in PT Sans; Lato / Inter / Source Sans 3 have larger x-heights
  but are not installed on this machine.
