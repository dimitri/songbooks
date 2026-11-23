# A band's votes, Discover and Influences

How a band decides which songs to add — the tool helps it decide; it
never decides nor enforces —, and what it listens to: its members
propose songs, the core members rate them, any member adds one or turns
it down; Discover and Influences are songs to hear, no vote. Code:
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

Each **core member** says **No thanks** — a veto, the strongest no — or
rates a proposal **1 to 4 stars**, an even scale never right in the
middle: 1 and 2 are against it, 3 and 4 for it. The vote control puts
No thanks first — a **thumbs down** (`icon-no-thanks`: the "not for
me" YouTube, Netflix and Pandora use, which everyone reads), drawn as
one of the stars: the same button, size, states (faint until chosen,
then the danger colour) and focus ring, its words in its label —, then
a gap, then the four stars; the same order wherever votes show (the
summaries, the queue, the history). Not voted is
"not voted", never 0 stars. A vote is changed or taken back until the
proposal is decided; every vote, change and take-back is kept
(`proposal_vote_log`). Keys: 1–4, N for no thanks.

**Guest members don't vote**: they propose, see the proposals and
listen; the API refuses their vote (403, "Guest members don't vote…").

### No one sees the others' votes before voting

Until a core member has voted on an open proposal (stars or no thanks),
they see only how many core members voted, of how many ("3/6 voted") —
no average, no for/against, no bars, no names, no one's no thanks, no
history of the others' votes. This is enforced by the server: the
tallies in the pages, the JSON the player and the party poll, the vote
answers, the proposal page's history (only one's own past votes), the
band's overview and the home page. In the list sorted by stars (or by no
thanks), the proposals one hasn't voted on come **first, apart**, newest
first ("Not voted by you yet"): their place among the others would tell
how those were voted. Taking one's vote back hides the others' again. A
decided proposal shows everything to everyone. A guest sees the votes
once the proposal is decided.

### The votes are information

The votes never accept or turn down a proposal by themselves. **Any
member (core or guest) decides by hand** — "Add to the songbook" (and to the set list
when that was its target; "Make it the reference" for another
recording) or "Turn down" — whatever the votes say, a veto included.
The confirm names who said no thanks ("Isla Garnier said no thanks. Add
to the songbook anyway?"); for one who hasn't voted, it says the votes
show once they vote (deciding doesn't reveal them).

What the votes say, once one may see them: the average (of the stars
given), for/against (3–4 / 1–2 stars), who said no thanks, who hasn't
voted, how many voted of the core members (no quorum). A proposal with a
no thanks is **flagged** (a red edge, the veto sign and count first in
its summary) and sorts after the ones without in the stars order. **"The
band likes it"** is a suggestion shown when the average is 2.5 or more,
with more votes for it than against and no veto.

The band setting of what a no thanks did (block, veto, count; migration
0127) meant something only while the votes decided; it's gone (migration
0129 drops `bands.proposal_no_thanks`).

Who may vote, decide and share is each in one helper
(`store.MayVote`: core members; `MayDecideProposal`, `MayShareListen`:
any member), to move to the band's roles (`HasBandPrivilege`) without
touching the handlers — a band could then keep deciding to its staff.

## Listening while voting

**Listen and vote** (`/bands/{id}/catalog/vote/listen`) is the set
lists' and songbooks' playlist player (`playlist.html`) with the open
proposals as its queue, in the order they came: each one's recording
(the band's, else the MP3 of its reference), else its YouTube video. The
stars are under the song playing (keys 1–4, N for no thanks), where its
vote stands beside it, each row's average and voted count in the queue.
What plays goes on in the mini player on the next page.

## Discover and Influences

Two more views of the Songbook, beside To vote, each a pool of YouTube
links to listen to (`band_listens`, migration 0129), in the same layout
and the same player:

- **Discover** (« Découvrir »): songs a member shares for the band to
  hear — no vote, no proposal, just "listen to this" — with who shared it
  and a note. **Propose for a vote** opens the propose flow on its link;
  the song then says "Proposed for a vote →".
- **Influences** (« Influences »): the artists and songs that shape the
  band's sound (what an original band sounds like), with what to listen
  for ("the drum feel", "the vocal harmonies") and tags.

Adding: paste a YouTube link — its title and artist are read from the
video (oEmbed), to fix —, a note, tags for an influence. **Any member**
adds, a guest too; who added a song removes it. **Play all** plays a
pool one after another in the playlist player, the video big beside the
queue on a wide screen.

There's no sync between devices: an earlier "listening party", where a
host's player drove everyone's phone, was not what the band needed and
is gone (migration 0129 drops `listening_party_now`). The calendar's
"Listening party" event type stays an ordinary event.

## The old votes and the migrations (0127, 0129)

The yes/veto votes became: **yes → 3 stars** (for it, the plain yes it
was; 4 is left for "love it", which a yes never said), **veto → no
thanks**. The vote as it was cast is kept (`legacy_vote`, and in the
history). A guest's old vote no longer stands (out of every average and
count) but stays in the history. 0129 drops the no-thanks setting and
the listening party's table, and adds `band_listens`.
