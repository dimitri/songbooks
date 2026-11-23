# A band's votes

How a band decides which songs to add: its members propose songs, the
core members rate them, the band listens to them together. Code:
`internal/store/band_proposals.go` (the rules), `internal/httpapi/band_proposals.go`
(pages and API), `static/js/band-votes.js` (the vote control, the
summary, the player's and the party's live parts), `static/js/proposal-flow.js`
(proposing a song), migration 0127.

## Proposing a song

The Songbook's **To vote** view → **Propose a song** opens a dialog in
three steps, the new song-adding flow (quick add, `quick_add.go`):

1. **Find**: a YouTube link (its title and artist read from the video,
   oEmbed) or "Title — Artist". The song the band may link is found (the
   band's own, else the catalogue's: same video, or same title and
   artist). A song already in the songbook takes a pasted video as
   **another recording** of it (a `new_reference` proposal: accepted, the
   video becomes its reference). Otherwise a **new song** is made for the
   band: private, the band's, in none of its songbooks until accepted;
   the video its reference, its recording downloaded behind (yt-dlp, for
   the player's MP3), its chart fetched.
2. **Check**: the new song's title, artist, label (to tell this
   recording from others), key and tempo, and its sections — renamed,
   reordered, added, removed, each one's chord grid rewritten — with how
   many lines of lyrics each holds (the lyrics themselves stay in the
   song, fixed later in its editor). A note "why this song", and where it
   goes once accepted: the songbook, or the songbook and one of the
   band's set lists (at the end of its last set).
3. **Sent**.

Closing the dialog before sending drops the song made for it (asked).

### Where the chart comes from

The editor's Import, as the quick add does (`songimport.VocalDraft`):

- **Lyrics and sections**: the best-rated Ultimate Guitar tab for the
  title and artist, its inline chords taken out; cross-checked against
  Genius's lyrics when found (else said so). Community transcriptions:
  usually right for well-known pop and rock songs, sometimes another
  language's version or a live take — hence the check step.
- **Chord grids**: Ultimate Guitar's chord lines turned into bars (the
  chords are generally right; the bar counts are inferred, the weakest
  part), replaced by **iReal Pro**'s grids section by section where its
  chart is a safe match (same key, same shape) — reliable bar by bar,
  but iReal Pro mostly has jazz standards and well-known songs. The
  check step says which sections have a grid and from where.

## Voting

Each **core member** rates a proposal **1 to 4 stars** — an even scale,
never right in the middle: 1 and 2 are against it, 3 and 4 for it — or
says **No thanks**, a refusal shown apart from the stars, never counted
as a star. Not voted is "not voted", never 0 stars. A vote is changed or
taken back until the proposal is decided; every vote, change and take-
back is kept (`proposal_vote_log`, the proposal page's history).

**Guest members don't vote**: they propose, see the proposals and their
votes, and listen; the API refuses their vote (403, "Guest members don't
vote…"). Only a core member accepts or turns down a proposal by hand.

Each proposal shows its average (of the stars given), for/against
(3–4 / 1–2 stars), how many core members voted of how many ("4/6"; no
quorum), who said no thanks, who hasn't voted. The list sorts by
**stars** (average, best first; then the most ratings), **newest**, or
**most no thanks**.

### What decides it

The votes decide once **every core member has voted**: in with an
average of **2.5 or more** and more votes for it (3–4 stars) than against
it (1–2 stars and no thanks); turned down otherwise. More than half of
the core members saying no thanks turns it down at once.

What one core member's **No thanks** does is the band's setting
(Settings → How the band votes; `bands.proposal_no_thanks`):

- **It holds the song back** (default): it can't go in — by the votes
  or by hand — while that no thanks stands; turning it down is still
  possible.
- **It turns the song down at once**: a veto (the old rule).
- **It counts as a vote against**.

## Listening while voting

**Listen and vote** (`/bands/{id}/catalog/vote/listen`) is the set
lists' and songbooks' playlist player (`playlist.html`) with the open
proposals as its queue, in the order they came: each one's recording
(the band's, else the MP3 of its reference), else its YouTube video. The
stars are under the song playing (keys 1–4, N for no thanks), where its
vote stands beside it, each row's average and voted count in the queue.
What plays goes on in the mini player on the next page.

## The listening party

The event type has a page: the Songbook's **Listening party** view
(`/bands/{id}/catalog/discover`, linked from a band's listening-party
event). One member **hosts**: plays the songs on the band's speakers
with the same player (`?party=1`), which says what plays
(`listening_party_now`). Every member's phone shows the song playing,
with the same vote control and summary, following the host every few
seconds; the queue below, every proposal with its vote.

## The old votes (migration 0127)

The yes/veto votes became: **yes → 3 stars** (for it, the plain yes it
was; 4 is left for "love it", which a yes never said), **veto → no
thanks**. The vote as it was cast is kept (`legacy_vote`, and in the
history). A guest's old vote no longer stands (out of every average and
count) but stays in the history.
