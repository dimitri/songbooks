# Import wizard and `sbcoach` song commands

The song page's Import tab brings a song in, or fixes one already there,
one song at a time. `sbcoach` runs the same steps from the command line
(internal/songimport does the work for both).

## Starting points

| Wizard | Command | What it does |
|---|---|---|
| Find & fetch automatically | `add --chart FILE` (a chart you have) | Ultimate Guitar's best chart, chords and lyrics, cleaned up, for review |
| Lyrics and iReal Pro grids, for singers | `add --vocal` | Lyrics and sections from Ultimate Guitar without chords; grids from iReal Pro where its chart matches (same key, strong match), Ultimate Guitar's elsewhere, said so |
| Add lyrics only | `lyrics --song ID` | Lyrics into the sections the song already has (below) |
| Reference recording | `add` (searched, or `--clip`) | YouTube's candidates, the artist's own studio upload first; downloaded, then split into voice and backing |
| Tempo, time, key and sync | `sync --song ID` | From the recording (below) |
| Fix chords with iReal Pro | `ireal --song ID` | iReal Pro's grid in the sections where it's a safe match; the others shown with how far they are |
| Clean up the source | (part of `add`) | Below |
| Check the key (review step) | `key --song ID` | Below |

The wizard never saves on its own: results go into the draft or into
Source, to check and save. The commands save a new revision (the song's
history has the previous one) and print what they did: sections, keys,
times and sources, never the lyrics. `--dry-run` shows without saving.

## Cleanup

`importer.CleanSongText`: noise lines (credits, tuning, "chords used"),
tab staff lines, chord-only rows left in lyrics, a "chords used" list
swallowed into the last grid, repeat annotations, labels repeated as
lyrics, extra blank lines, empty sections; a capo line becomes the
`capo:` header. Every fetched draft goes through it; the wizard's
buttons run it on a draft or on Source.

## Lyrics into sections

`importer.SplitStanzas` and `PlaceLyrics`: the fetched lyrics in
stanzas (English and French labels: Couplet, Refrain, Pont…), then the
n-th verse stanza into the n-th verse, the chorus into every chorus,
unlabelled stanzas in order; instrumental sections stay empty, lyrics
already there are kept unless replaced. The wizard lists where each
stanza went before anything goes into Source. Sources: the best Ultimate
Guitar chart's lyrics, a lyrics page's address, or pasted text.

## Tempo, time signature, key and sync

`audioanalysis` decodes the recording and its voice and backing parts
(ffmpeg) and measures: where the sound starts, the tempo, the metre, the
key (from the backing), and where the voice comes in. `songimport`
places the chart's sections (numbered as the player numbers them,
`httpapi.SyncSections`) on the voice: sung sections over the singing,
instrumental ones between, each sung section snapped to the nearest
vocal entry. The time signature is the chart's own `time:` when it has
one, else iReal Pro's, else the recording's estimate. Saving writes
`tempo:` and `time:` (replacing the chart's own only when asked) and a
marker per section, from the intro on.

Before the voice is split (a few minutes after the download), sections
are placed by tempo only; run it again once it's split.

## Key check

The chart's key (its `key:` header, else its chords), with its capo the
key it sounds in, against iReal Pro's and the recording's: a relative
minor counts as the same key; otherwise it says by how many semitones to
transpose the chart to match the recording.

## Lyrics and rights

Fetching is one song at a time, for the person's own use. Putting lyrics
in a community's songbook that people pay for is publishing them: in
France that needs the rights holders' licence (SACEM for the lyrics), as
any paid service showing lyrics does.
