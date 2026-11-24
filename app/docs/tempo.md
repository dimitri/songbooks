# What a chart's tempo counts

A chart's `tempo:` counts the **beat of its `time:`**:

| `time:` | the beat | `tempo: 60` is |
|---|---|---|
| 4/4, 3/4, 5/4 … (and none) | a quarter note ♩ | 60 quarters a minute |
| 2/2, 3/2 | a half note | 60 halves (120 quarters) |
| 6/8, 9/8, 12/8 (15/8 …) — compound | a **dotted quarter** ♩. | 60 dotted quarters (90 quarters, 180 eighths) |
| 3/8, 5/8, 7/8, 11/8 … | an eighth ♪ | 60 eighths (30 quarters) |

The compound rule is the musicians' convention: a 6/8 bar is two beats.
The chart corpus was written that way (its 6/8 songs say 60 to 72, its
12/8 ones 58 to 110), and so were the parts that read it: the player's bar
grid, its click and count-in, the metronome, the set list's click track
(`clicktrack_meter.go`), the recording's analysis and the import wizard's
sync (`chartalign.BeatsOf`, `audioanalysis.BeatsPerBar`), the PDF's and the
jazz chart's ♩. = mark. `internal/song/tempo.go` (Go) and
`static/js/tempo-units.js` (JS) say it once: `CompoundTime`,
`BeatQuarters`, `QuarterTempo` / `FromQuarterTempo`, `BeatNote`.

## At the boundaries

Guitar Pro, alphaTab, the Mixer's auto-align (`mixer-autoalign.js`) and
LilyPond count **quarter notes**. Their tempo is converted where it crosses
over (`× BeatQuarters` out, `÷` in):

- the Guitar Pro import's header (`gp-import.js`): a 6/8 file at ♩ = 69 is
  `tempo: 46`;
- the "Out of step with the Guitar Pro file" panel (`gpdrift.go`): compared,
  taken and pushed in the song's beat;
- a blank Guitar Pro file made from the song (`gpview.go`);
- the Mixer's `/mixer` answer (`"tempo"` is in quarters) and the alphaTeX it
  plays (`handlers_mixer.go`), and the auto-align's bpm said in the chart's
  beat (`mixer.js`);
- the player's own downbeat detection (it hands the auto-align quarters) and
  the Guitar Pro file's tempo while its synthesizer plays (`audio-player.js`);
- LilyPond: `\tempo 4. = 46` in 6/8 (`lilypond.go`).

Until 2026-11-24 these crossed unconverted: a 6/8 Guitar Pro import wrote
the file's quarters into `tempo:` (half again too fast for the player), the
drift panel always flagged a compound song's tempo, the Mixer played a 6/8
chart at two thirds of its tempo, and the player looked for a 6/8
recording's downbeats at two thirds of it (the Sync sheet's snapping and
"Suggest" with them).

## Saying it

Wherever a compound chart's bpm is shown it says its note: `♩. = 46 (♪ = 138)`
(the Sync sheet, the player's sync warning, Tap tempo), `♩. = 46` where room
is short (the song's header, the editor's toolbar, the speed and click
labels). A simple metre keeps `46 bpm`.

Where a compound chart's tempo is typed or tapped (the editor's Tempo
field, Tap tempo, the Sync sheet's Chart tempo), the note can be picked
beside it — dotted quarters, quarters or eighths, remembered in the browser —
and is saved in dotted quarters.

## The Sync sheet

Its counter runs at the tempo the chart's bars make between two section
starts (`sync-wave-math.js rowTempos`, the bar-weighted `medianTempo`); a
section more than 8 % off the chart's tempo is flagged. The sheet's
**Chart tempo** sets `tempo:` from there (typed, tapped while the recording
plays, or the warning's one click: "Use ♩. = 46 as the chart's tempo"),
saved as any edit of the song (`PUT /api/songs/{id}/meta`: who may edit the
chart, The Big Songbook's review, a revision); the player's grid outside the
starts, the click, the snapping downbeats, the checks and the analysis
follow without a reload, the unsaved sync left as it is.

A section's **bar count** isn't edited from the sheet: it is the chart's
grid (its bars, repeats, `times:`, a bare section borrowing an earlier one's
bars), and which bar to add or drop is a musical choice. Where the
recording's analysis knows what is played, its "written N · played M: use
the played ones" proposal rewrites that section's grid (reviewed, then saved
as a revision); otherwise, the editor.
