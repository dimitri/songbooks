# A band's recordings

A band shares the folder it keeps its recordings in, on pCloud, and the app
indexes it: each rehearsal and concert, its recordings in the order they were
played, to listen to, rate and choose from. Nothing is copied. The recordings
play straight from pCloud: the browser asks pCloud's public API for each file's
link (`getpublinkdownload`, allowed from any site, the link good for the
browser's own address), and the audio never goes through the app. That request
goes without a Referer (`referrerPolicy: 'no-referrer'`): with an Origin and a
Referer from another site than pCloud's (localhost aside), pCloud refuses it
("Invalid link referer", error 7010).

## Setting it up

One member pastes the folder's pCloud shared link on the band's Rehearsals tab.
The app lists the folder straight away (`showpublink`: the whole tree in one
call), and again on *Read it again*. Files gone from the folder are kept, marked
gone, with their stars, in case they come back.

## What a recording is

From where it is and what it's called (`store.parseRecording`), the way bands
file them:

```
Repetes/2025-03-13/07 - Plan Chicago Blues - Take 2 - 2025-03-13.mp3
Concerts/2025-06-14/03 - Killing Me Softly - 2025-06-14.mp3
Auditions/2024-07-25/1-1 - Thierry - You Know Im No Good - 2024-07-25.mp3
```

- **Kind**, from the top folder: `Repetes`/`Rehearsals` a rehearsal,
  `Concerts`/`Live` a concert, `Auditions`, `Enregistrement`/`Studio`,
  `Albums`, `Set List`; anything else, other.
- **Session date**, from the nearest dated folder (`2025-03-13`,
  `2024-03-16 - MJC Fresnes`, a month alone is its first day), else the file's
  own date.
- **Name**, read with a template (`recording_names.go`), as Audacity's
  *Export Multiple* names a file per label: `{n}` the song's place, `{set}`
  the set, `{title}`, `{take}` after "Take" or "Prise", `{date}` (2025-03-13,
  20250313, 250313, 13-03-2025), `{*}` anything else; dashes, underscores and
  spaces are loose. The band's own templates first (the Corrections page:
  *Try it* shows what one reads in the folder, *Add it* reads every name
  again), then the built-in ones, most specific first:
  `{set} - {n} - {title} - Take {take} - {date}`,
  `{set} - {n} - {title} - {date}`, `{set}-{n} {title}`,
  `{n} - {title} - Take {take} - {date}`, `{n} - Take {take} - {title} - {date}`,
  `{n} - {title} - {date}`, `{n} - {title}`, `{n}-{title}`, `{n} {title}`,
  `{title} - {date}`, `{date} {title}`, `{title} Take {take}`… The band's name
  and a version before the title (`v2-Gone`) say nothing.
- **A recorder's own names** (`TRACK01`, `MASTER`, `ZOOM0004`, `DR0000_0001`,
  `250505_220154`, `IMG_2602`, a phone's `New Recording 12`, a copy's `… 2`) and
  a video whose name numbers no song (a trailer, a stream) are listed but never
  asked about: raw.
- **Song**: the band's song (its default songbook and its own songs) the title
  names, give or take accents, apostrophes, spaces, a number for a word
  (`Teens4Today`) and a letter or two; else another
  part of the name (an audition names the candidate first); else the song whose
  every word the name has (`Other Lies-Ultrablanc-Colors_of_Thunder_115_Mix`).
- Skipped: what isn't audio or video, a DAW session's parts (`Audio Files/`,
  `*.band`, `*.logicx`), a take's stems (a folder named like a take).

## Corrections

What names get wrong, corrected once and applied at every reading
(`band_recording_fixes`):

- **Dates**: a folder whose files are named with another date is a question
  (`2025-09-15/` of files named `2026-09-15`): one answer is the folder's, the
  other its files'; the one nearest to when the files went up on pCloud is
  marked likely. A date can also be corrected by hand.
- **Titles**: a title that names none of the band's songs (`Kawazaki`, a song
  not in the repertoire yet, a jam) can be said to be one of them.
- **Files**, one by one: a recording whose name gives no song (*Files with no
  song*; the recorder's own files on demand) is said to be one of the band's
  songs, or not a song. A file's correction comes before its title's.

## Rehearsals and Concerts

Each tab: a day's recordings in the order played (the latest day first), a
month calendar of the days recorded, every day's list, and a player (playing
the day through, from the lock screen too). Every member gives a recording ½ to
5 stars; the band's average shows by it.

The player, on a phone, is a mini player over the tab bar (the day's date,
the title, play, +15 s, a thin progress line); tapping it opens the full
player: the stars, the scrubber, previous, −15 s, play, +15 s, next, Restart,
the speed (0.5× to 1.25×, the pitch kept) and a loop between two points
(Loop A–B: a tap marks A, the next B, the next stops looping), and the
day's recordings to pick from. A wide screen shows it all in one bar, which
opens (its title, or its ⤢ button) into the same full player as a panel in
the middle of the screen: on a tablet in portrait, one column; on a
landscape screen (an iPad held sideways, a desktop), the player and the
recordings side by side. Escape, its × or a tap beside it closes it. Keys: space or K plays and pauses, ←/→ or J/L
skip 15 s, shift ←/→ the recording before or after, 0 restarts.

The same player plays a set list (its running order page): each song's
sound in the set's order, *Play all* or a song's own play button. A song
has up to two: our recording (the band's chosen recording from its pCloud
folder, the band's reference below, else the song's own recording) and the
reference (the YouTube video's sound). *Our recordings | Reference* picks
which plays, the other when a song has only one; the choice is kept for
the next visit, and switching while a song plays starts it again in the
other version. A song with neither keeps its link to the video. The lock
screen keeps previous and next, and scrubbing.

The Rehearsals tab also says how the songs were rehearsed: the most and least
played, the longest not played, the best rated, the songs never recorded at a
rehearsal, and every song's count, last date, stars and concerts.

## The band's reference

A song's recordings page lists the band's recordings of it, the best rated
first, and one can be chosen as the band's reference for the song. The band's
playlist plays it, before any other recording (*Ours*).
