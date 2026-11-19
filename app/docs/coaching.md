# A coaching community

A voice coach and their students (docs/singing-teachers.md for the market
behind it). A community whose kind is `coaching` (migration 0079): its
staff is the coach, every other member a student. Set up from the command
line for now (`cmd/sbcoach`), run from its pages after that.

## Students (the coach) / My voice (a student)

`/communities/{id}/students`: a card per student, their voice on a strip
from C2 to C7, how many songs, their next lesson. A student's page
(`/communities/{id}/students/{personID}`, the student's own "My voice"):

- **Their voice**: a range per register, chest, mixed and head, from a
  lowest to a highest note, and a word on each; registers overlap (the
  mixed voice bridges the two), drawn as three bars on the strip. The
  coach changes them there (saved as changed); the student reads them.
- **The coach's notes**: free text, the coach's only.
- **Their songs**: the student's songbook, kept by the coach (a songbook of
  kind `student`, gated to that student with a grant, as a paid songbook
  is). A song there is the community's song; only its key is the
  student's (`songbook_entries.semitones`, ±12, the coach's −/+). Opened
  from there, the song page starts in that key: the practice player's Key
  (docs/stems-and-key.md), the chords following. A student may still have
  songbooks of their own; this one is the coach's.
- **Their lessons.**

The coach adds songs from the community's other songbooks (the
Repertoire, the coach's reference material). A recording of a coaching
community's song is split in two parts only, voice and backing (the stems
worker keeps vocals and no_vocals): the Voice control and the Mixer's
Vocals and Backing lanes.

## Groups

`/communities/{id}/groups`: a year's class (September to June, the
dates its own) or a one-off around an event (a master class, a "stage":
community events of type `masterclass`, which the group's members are
registered for). A group has its members (the coach's to change), its
songs in the group's key, and its sessions (lessons for the group). Who
joins gets the group's songbook and its coming sessions; who leaves loses
both. A student sees their groups.

## The songbooks

Four places a song can be, each for its own use:

- **The Repertoire** (the community's default songbook): the coach's
  reference, every song in its original key.
- **A group's songbook** (kind `group`, its members granted): what the
  group works on, each song in the group's key.
- **A student's songbook** (kind `student`, the student granted): what the
  student is given to work on alone, each song in their key.
- **The student's own songbooks**, outside the community: theirs.

A song is the community's song in all of them; only its key differs
(`songbook_entries.semitones`). Opened from a group's or a student's
songbook, the song page starts the practice player in that key, the
chords following. A student's page shows their songs and their groups'.

## Students' songs, and an event's program

The coach's **Students’ songs** tab (`/communities/{id}/student-songs`) lists every song the students and groups
have (their songbooks), each with who has it and in which key, as chips:
"Emma A (+2) · Les Voix G (original)". A filter matches title, artist or
singer. Ticking songs and **Plan for an event…** adds them to an upcoming
event's program (an open mic, the end-of-year concert, a master class),
or to a new one made on the spot.

The program (`/communities/{id}/events/{eventID}/program`, tables
`event_program` and `event_program_singers`, migration 0082) is the songs
in running order, each in a key, with one lead singer and any backing
vocals:

- Picking the lead puts the song in the lead's key when they have it; a
  song only one student has starts with them as lead, in their key.
- The coach moves the key by a semitone, reorders, takes songs out, adds
  songs from the repertoire; "Who sings what" counts each singer's leads
  and backing parts.
- Students see the program read-only, their own parts marked.
- A song opened from the program (`/song/{id}?event={eventID}`) plays in
  the program's key, the chart and the recording, says whose key it is
  (the event's), goes back to the program, and steps through it with
  previous and next.

## Teaching notes

On a student's page, the coach's only: a few lines about the student (who
they are as a singer, goals), and dated notes, each maybe about a lesson
(what was worked on, what to practise next), the latest first, edited in
place.

## Practice

What a student works on between lessons (migration 0084, practice.go):

- **An assignment**: one of the student's songs (or their group's), or
  something in words ("lip trills, five minutes"), with what to work on,
  so many days a week, until a date. The coach gives it on the student's
  page (or the group's, for all its members), marks it done or removes
  it.
- **The student** sees *To practise* on *My voice*: each assignment, its
  days this week against the target (2/3), until when, the last time.
  The song opens from there in their key.
- **Logged by itself**: a song opened from the student's songbook (or
  their group's) counts the time its recording plays (practice-time.js,
  one row a song a day), on the open assignment of that song. The coach
  playing it counts nothing.
- **Told**: *I practised*, so many minutes, a day of the last 30, how it
  went; on an assignment, or something else.
- **The coach** sees each student's last four weeks on their page, and in
  the students table and cards: minutes and days in the last seven, what's
  left to practise.

## Checking one's singing

*Check my singing* under the song page's player (pitch-check.js), for
any song whose recording has its voice apart:

- **The line to sing** is the recording's own voice part, tracked note by
  note (YIN) in the browser, once per recording (kept in IndexedDB), and
  cut into notes: steady stretches of 0.12 s or more.
- **The singer** sings over the recording, headphones on and the voice
  down, in their key (the Key moves the line too); the microphone is
  tracked the same way. Drawn as it's sung: the notes as bars, coloured
  once passed, the singer's pitch as dots.
- **Scored per note**, as research scores singing (a note in tune within
  so many cents of its target, octave aside): the median of its middle
  (its edges carry its neighbours) against the note. Three levels, the
  app's from what listeners hear (non-musicians call a note out of tune
  from about 20–30 cents, experts from about 10): *Beginner* within 50
  cents (research's "accurate" singing; past 100, research speaks of
  "poor-pitch" singers, 10–20% of adults), *Intermediate* within 25 (what
  anyone hears), *Advanced* within 15 (a trained ear; the tracking's own
  error is near 10 cents, so a tighter margin faults a perfect take). A
  note within twice the margin shows amber: close.
- **Placement**: each entry after a rest, against the take's own median
  (the device's latency, which no browser reports exactly, cancels
  out): on time within 250, 160 or 110 ms (entries are measured to about
  ±60 ms). Steadiness, not early or late.
- **A take** ends at a pause: three stars, the notes in tune and on time,
  the tendency (flat, sharp) and the steadiness in words, the passages
  that slip (a click goes there).
- **Kept** for a coaching student singing a song of their songbook
  (migration 0085): every take (practice_takes), the day's last scores
  on their practice log.
- **The coach's settings** (`/communities/{id}/checking`, linked from the
  Students tab and the goal form; migration 0086): each level's name and
  margins (cents, ms), and the words after a take, by situation (on the
  notes, flat, sharp, far, steady, uneven, three stars, no star yet, a
  personal best, the goal reached), the app's filled in as examples to
  rewrite, with `{name}`, `{cents}`, `{ms}`, `{pitch}`, `{rhythm}`. Words
  the coach wrote are signed with their first name. A take keeps the
  margins it was judged with.
- **The coach's goal**: on an assignment of a song, a level and the share
  of notes in tune and on time to reach. The student sees it as two
  bars filling with their best take; reaching it is marked on the
  assignment (and celebrated). The panel shows the last takes as little
  bar charts, the days of practice in a row, and a personal best.

## Lessons

`/communities/{id}/lessons`: the coach's calendar, day by day. A lesson
(an event of type `lesson`): when, how long, in the room or online, where,
a name, and either a group (its members come) or students one by one,
registered by the coach (for now). A student sees their own lessons
there, on their page, and on their home calendar; the coach sees them
all. Lessons stay off the community's Events (and their pages, and its
overview), which keep everything else: concerts, jams, meetups, master
classes, and open mics (`open_mic`, for any community or band).

## Everyone's calendar, in their calendar app

`/calendar/subscribe`: each person's calendar as an iCal feed (gigs,
rehearsals, events, lessons, three months back to a year ahead) at a
secret address (`/calendar/feed/{token}.ics`): *Subscribe on this device*
(webcal), Google Calendar, or the address to paste; a new address can be
made, the old one stops. A lesson says whether it's online; its link goes
to the Lessons page (and, with Galène, would join the lesson).

## The look

Like any community's (docs/mobile-and-look.md): Chloé Paul's takes the
mood of Céline Dion's site, not its marks: light grey and white, near-black
text, Montserrat throughout, a dark header, a blue accent and a gold
second accent, square shapes, "CP" for a mark. Montserrat and Cormorant
Garamond joined the fonts a look picks from.

## Setting one up

```
sbcoach community --db URL --name "Chloé Paul" --coach chloepaul --student tapoueh --look look.json
sbcoach songs --db URL --community ID --student tapoueh --songs 206,215,228 --clips clips.tsv
```

`community` makes the coach's account when there's none (no password, no
email: theirs once invited), the community, its Repertoire, its look, and
a first student. `songs` copies songs into the community (its members may
open them, a person's own songs are private), puts them in the student's
songbook, and with `--clips` (song id, tab, YouTube URL) sets each one's
reference video and downloads its sound, which the stems worker splits.

## Warm-ups

The *Warm-ups* tab (exercises.go, warmup.js; migration 0087): the coach's
exercises, written once, sung by each student in their own keys.

- **An exercise** is a pattern: the written key's tonic, a scale (major,
  minor, chromatic), the notes as degrees (`1 2 3 4 5 4 3 2 1`, 8 the
  octave, `r` a rest), their lengths (`8 8 8 8 4`, the last one goes
  on), the syllables sung (cycled), the piano (playing the line, holding
  the chord, silent), a chord before each repeat (I, or V7 then I), the
  tempo, a breath after each repeat, a semitone or a tone between keys.
  Or a *rhythm*: entries on one note, repeated, scored on placement
  only. Classics to start from (five-note scale, 1-3-5-8-5-3-1, staccato
  arpeggio, octave scale, siren, lip trill, a syncopation), heard and
  seen as they're written.
- **Written out by the browser**: every repeat in its key, as one
  MusicXML score of two parts, the line with its syllables and the
  piano, that alphaTab draws and plays with the soundfont; and every
  note's exact time and pitch for the checker. No file to keep.
- **The coach's demo**: *Record my demo* plays the written key once and
  records the coach singing over it (the microphone as it is); students
  *Listen to* it to hear how it goes. Not to train with: training is the
  piano, in their keys.
- **A student's path**: waypoints in semitones from the written key,
  walked a step a repeat. The app's, from their range: the pattern
  fitted inside it, up to its top and back, at most eight steps up. The
  coach sets another per student (*In the keys of …*); the student can
  move it an octave. Keys named as the page's language says them (do ré
  mi, middle C do3).
- **Checked** as songs are (pitch-check.js), against the exercise's own
  notes: the coach's levels and words, or the exercise's own (its level,
  and words written for it over the community's); every take kept
  (practice_takes.exercise_id), the day's practice logged, the goal of
  an assignment of it reached.
- **Assigned** like a song: a student's or a group's practice can be an
  exercise, with a goal.
- **Sessions**: exercises in order, played through as one (*Next: …*),
  the stars of the run added up (out of three an exercise).

## Review: live lessons with Galène, inside the app

Being built (docs/live-lessons.md: the deployment, the firewall, why
Caddy and no HAProxy). Research of November 2026 against Galène 1.2.2
(9 November 2026); notes and sources in `research_notes/Galene
deployment/` (not in the repo). [Galène](https://galene.org/) (MIT, Go,
Juliusz Chroboczek) is a videoconference server: an SFU, one binary, a
web client, JSON group files, a built-in TURN server, an admin API, and
JWT tokens it checks against keys we give it.

### Deploying it in our compose stack

- **No official image**: a small Dockerfile of ours builds it
  (`CGO_ENABLED=0 go build`, and `galenectl`), with its `static/`.
- **Host networking** (`network_mode: host`): Galène can't be told a
  public address for its media, and its TURN server only offers the
  host's own interfaces; behind Docker's NAT media breaks.
- **Flags**: `-insecure -http 127.0.0.1:8444` (Caddy terminates TLS),
  `-udp-range 40000` (one port, multiplexed, since 1.0), `-turn auto`,
  `-data`, `-groups`, `-recordings` on volumes (recordings shared with the
  app read-only).
- **Caddy**: a subdomain, `live.songbooks.studio { reverse_proxy
  127.0.0.1:8444 }` (the websocket at `/ws` passes as is).
- **Firewall**: TCP 443 (Caddy), UDP 40000 (media), TCP and UDP 1194
  (TURN); 8444 stays closed.
- **Config**: `data/config.json` with `proxyURL`, `canonicalHost`, an
  admin user (setup only); one group file `groups/lessons.json`:
  `auto-subgroups` (a lesson is `lessons/<uuid>`, made at its first
  join), no recording for now, Opus, our key in `authKeys` (HS256), no
  users and no wildcard user: only a token gets in.
- **Resources**: 256–512 MB; a 1:1 lesson costs nothing, 1:10 all on
  camera is about half a core and up to 77 Mbit/s out (simulcast less).

### Join links, from the app and the calendar feed

Built (internal/httpapi/live.go, migration 0083). What a coach and a
student see:

- **The coach** makes a lesson *Online* (the lesson form's *How*, or
  later, the lesson's *Edit*: back in the room just as well). It's
  tagged *Online* on the Lessons page, the student's page and the
  calendar.
- **On the Lessons page**, an online lesson to come has a *Join*
  button, lit (*Join the lesson*) from 15 minutes before it starts to 30
  minutes after it ends; earlier, the button leads to a page saying when
  it opens. The coach's and each student's own.
- **In the calendar app**, the lesson's link is the join link, carrying
  the person's feed address (`?f=`): a tap on the phone goes straight in,
  without logging in.
- **Headphones**: a switch above the lessons, *I join online lessons
  with headphones*, remembered per person (`people.live_headphones`);
  `?headphones=1` or `0` on a join link changes it too. With it, Galène
  sends the voice as it is, in high quality; without, its echo and noise
  removal, right on laptop speakers. Everyone joins muted.
- **In Galène**, the person's name is already filled in: one click on
  *Connect* (with or without camera and microphone). The coach can mute
  or remove someone and hand out a link of their own; a student shows
  themselves and talks.
- **A lesson's room** is made the first time someone joins
  (`events.live_room`), so a lesson moved to another day keeps it.

The design, as reviewed:

- **The app signs the tokens**, Galène checks them: a JWT (HS256, our
  `kid`), `sub` the person's name, `aud`
  `https://live.songbooks.studio/group/lessons/<uuid>/` (exactly, trailing
  slash), `permissions` `["op","present","message","caption","token"]`
  for the coach, `["present","message"]` for a student, `exp` two minutes
  ahead.
- **The link in the calendar is ours**, not Galène's:
  `https://app.songbooks.studio/communities/{id}/lessons/{event}/join?f=<calendar feed token>`.
  It checks the person is the lesson's coach or student and that it's
  time (15 minutes before to 30 after; otherwise a page saying when),
  mints the token, and redirects to
  `https://live.songbooks.studio/group/lessons/<uuid>/?token=<JWT>`: no
  password, one click to start the media. It works weeks later, follows
  a rescheduled lesson, stops with a new feed address (the feed token
  identifies the person), and no long-lived Galène token sits in
  calendars. In the app, the Lessons page's *Join* goes the same way
  (logged in, no `f=`).
- **A lesson online** gets its `uuid` (a column on the event) when made;
  the calendar feed's description and URL carry the join link.

### A singing voice

The built-in client turns on noise suppression (with echo cancellation
and gain control) and limits Opus to its normal quality, mono. For
singing: a patched `static/` (`-static`) with preprocessing off, high
quality audio on (Opus up to 128 kbit/s) and stereo capture; headphones
then. Decided: headphones optional, Galène's defaults without them, and
the singing settings with `?headphones=1` on the join link (the person's
choice, remembered by the app); everybody joins muted
(`deploy/galene/client-hook.js`, patched into the client at build). Our own client later, on Galène's `protocol.js`, beside the song or
the warm-up being worked on (score, key, voice): Galène can be framed
(no `frame-ancestors`), untested.

### Recordings

Not for now (decided). Later: the coach records (`record` permission): a WebM per participant
(`recordings/lessons/<uuid>/`), Opus audio; the app reads the shared
volume, mixes coach and student with ffmpeg, and attaches the result to
the lesson (and the student's teaching notes).

### Latency

WebRTC, 100–300 ms: talking, demonstrating, call and response are fine;
the coach accompanying the student live in time is not. The student sings
over the backing on their side (our player, in their key), mixed into
what they send.

### Effort

The image, compose, Caddy and firewall: half a day. Tokens, join links
in the app and the feed: 1–2 days. The singing client defaults: half a
day. Recordings back to the lesson: 1–2 days.
