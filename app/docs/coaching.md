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

## Who sees what

A coaching community's pages are the coach's or the student's
(coaching_viewer.go): the staff (a manage privilege) see the coach's; a
student sees the student's, their own data only, **a site admin who is
a student there too** (the app's owner, taking lessons); a site admin
who isn't a member sees the coach's.

**See as…** (site admins only, on every page of the studio: the
Songbooks tab, the student pages, the lessons, groups, warm-ups, the
overview): as themselves, as the coach, as one student by name, or as a
member with nothing yet. It's remembered (a cookie, `coach_see_as`,
for that community) until they leave it, and a striped banner says it's
a preview. As a student or a member it's **read-only**: every change
asked for on the studio's API is refused (409) while it's on, so
nothing is asked, logged or changed for someone who didn't. As the
coach, the admin acts as themselves, with the coach's pages. Anyone
else's cookie counts for nothing, and the switch (`GET
/communities/{id}/see-as`) answers 403.

## Voice types

On a student's page, under *Voice* (the C2–C7 strip): the coach's
assessment (migrations 0108, 0109, 0111; coaching_voice.go,
store/voice_profile.go). The range comes first, the label second: most
coaches of pop singers keep no fixed label (see below).

- **The range** leads ("Range do2–la4"): the registers' (chest, mixed,
  head), as before, no second range. The registers are named the way a
  pop coach also says them: *Chest / belt*, *Mix*, *Head / falsetto*.
- **The comfortable zone** (the tessitura), beside it ("comfortable
  sol2–la3"), a striped bar under the registers. In French *zone de
  confort*, since the app already says *tessiture* for the range
  (French speakers often do; strictly it's the *étendue*).
- **The working label**, second and optional: *not classified yet*
  unless the coach says otherwise; a type (soprano, mezzo-soprano,
  contralto, countertenor, tenor, baritone, bass; French: soprano,
  mezzo-soprano, contralto, contre-ténor, ténor, baryton, basse),
  either *leaning* (a working opinion, the default: "leaning baritone ·
  Nov 2026", *plutôt baryton · nov. 2026*) or firm ("Baritone · Nov
  2026"), dated by its last change. Types noted before 0111 stay firm.
- **A few words beside it**, each optional: the timbre (bright, warm,
  dark) and where the two passages sit ("passages at mi3 and si3").
- **The assessment**: a few words (what she hears, what to work on).
- **The history**: every change to the label, the comfortable zone,
  the timbre, the passages, the assessment and each register's range,
  with when, who, old → new and the note; the latest first.
- **The student** reads it all: their range and comfortable zone, the
  label, the assessment and its history (migration 0109 dropped the
  switch that kept the assessment the coach's: it's what they make
  progress with).
- **The coach's cards** (the Songbooks tab) show the range and the
  comfortable zone, then the working label.

### The song's melody over the student's range

Where a key is chosen for a student, the student's range is drawn as on
their page (C2 to C7: chest, mix, head, the comfortable zone striped),
and the song's melody over it as a frame, **at the key being tried**: it
slides as − / + is pressed. Information, not advice: no verdict, no
number, only the caption *the song's melody (from its recording), in this
key*. Nothing when the melody isn't known (the coach's pickers say
*melody range not known yet*). Where:

- the song page's *Voice & key* panel (a coaching community with its key
  per student on): the student the coach picked, or the student's own;
- the coach's *Add to…* (a student picked) and the requests' key
  pickers, on the Songbooks tab.

`GET /api/songs/{id}/voice-strip?community=C[&person=P]`
(voice_strip.go, voice-strip.js): without `person`, the viewer's own
(a student; the student a site admin looks as); with it, the coach's
only; the song must be the community's. The melody's place in each key
from −12 to +12 comes with it.

**The melody range** is found from the recording's vocals part
(audioanalysis.MelodyRangeOf: the voice tracked note by note with YIN,
only notes held about 0.14 s or more, from the 5th to the 95th percentile
of their pitches, so an octave slip, a shout or a backing voice's stray
note don't move it; at least 8 s of steady singing, else none). The app
finds it in the background (melody_ranges.go, started by cmd/songbooks),
one vocals part at a time, newest first, never while a page waits, and
keeps it (`song_melody_ranges`, migration 0112): once per split; a new
split (a new vocals part) or a new analyzer finds it again; a part that
can't be read is kept as "no range" until it's split again. A song's
range is its reference recording's (YouTube), else its band recording's,
else a Guitar Pro backing's (rarely sung): the recordings the player
offers. It's the recording's own key, the one the key's semitones move.
About half a second per recording (Apple M-series, a 3–4 minute part).

### What the types mean

Classical singing names six or seven types (opera; choirs use the four
SATB parts), the German *Fach* system subdividing them (coloratura,
soubrette, lyric, spinto, dramatic; Heldentenor, Kavalierbariton…).
Typical ranges and passaggi (Richard Miller's, as Wikipedia reports
them; sources differ by a semitone or more):

| Type | Typical range | Primo / secondo passaggio |
|---|---|---|
| Soprano | C4–C6 (coloratura up to F6) | E♭4 / F♯5 |
| Mezzo-soprano | A3–A5 | E4 / E5–F5 |
| Contralto | F3–F5 | G♯4–A4 / D5 |
| Countertenor | about E3–E5 | no standard values |
| Tenor | C3–C5 (some: B2–B4) | about C4–E4 / F4–A4 |
| Baritone | A2–A4 (some: G2–G4) | lyric B3 / E4 |
| Bass | E2–E4 | G3–A3 / C4–D4 |

A type is more than a range: teachers weigh the **tessitura** (where the
voice is comfortable and sounds best, individual, no standard values),
the **timbre**, where the **passaggi** fall, and the voice's weight and
agility. Singing long outside the tessitura (pushing chest voice up
especially) strains the voice.

Coaches of pop, rock and musical theatre (CCM) classify by range, timbre
and where the voice changes register, but most keep no fixed *Fach*: a
label is a working description that changes with training, and an
untrained voice (chest carried too high, falsetto leaned on) can't be
classified before its registers develop. Hence *not classified yet*,
the assessment in words beside the type, and the history.

French: *tessiture* (the comfortable notes; loosely, the range),
*étendue* (the whole range), *passage* (passaggio), *registre* (voix de
poitrine, voix mixte, voix de tête); *haute-contre* is a historical
French high tenor, not the modern *contre-ténor* (mostly falsetto).
French octaves: middle C (C4) is do3.

Sources: [Voice type](https://en.wikipedia.org/wiki/Voice_type),
[Passaggio](https://en.wikipedia.org/wiki/Passaggio) (values from R.
Miller, *The Structure of Singing*, *Training Tenor Voices*, *Securing
Baritone, Bass-Baritone, and Bass Voices*),
[Fach](https://en.wikipedia.org/wiki/Fach),
[Tessitura](https://en.wikipedia.org/wiki/Tessitura),
[Countertenor](https://en.wikipedia.org/wiki/Countertenor),
[Voice classification in non-classical music](https://en.wikipedia.org/wiki/Voice_classification_in_non-classical_music);
I. Bartlett & M. L. Naismith, "An Investigation of Contemporary
Commercial Music (CCM) Voice Pedagogy: A Class of its Own?", *Journal of
Singing* 76/3 (2020),
[NATS](https://www.nats.org/_Library/JOS_On_Point/JOS-076-3-2020-273_-_Bartlett-Naismith_-_An_Investigatin_of_CCM.pdf);
A. Young, "Mixed Up and Upside Down", *Journal of Singing* 76/2 (2019),
[NATS](https://nats.org/_Library/JOS_On_Point/JOS-076-2-2019-137_-_Mixed_Up_Registers_Young.PDF);
A. Festeu, ["Voice classification: system or art?"](https://blog.oup.com/2018/03/voice-classification-system-singing/),
OUPblog (2018); A. C. Merritt,
["Determining Your Voice Type"](https://www.interlochen.org/online/determining-your-voice-type),
Interlochen; [Voix (musique classique)](https://fr.wikipedia.org/wiki/Voix_(musique_classique)),
[Tessiture](https://fr.wikipedia.org/wiki/Tessiture),
[Haute-contre](https://fr.wikipedia.org/wiki/Haute-contre) (French
Wikipedia); [Voix](https://www.larousse.fr/encyclopedie/musdico/voix/170577),
Larousse, *Dictionnaire de la musique*.

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

The **Songbooks** section (`/communities/{id}/songbooks`,
coaching_songbooks.go) shows them from each side:

- **The student**: first *Your songbook with* the coach (its name, how
  many songs, in their key, the next lesson), each song with its key as
  a pill ("C (−3)", or "original"), *All n songs* and *PDF in my keys*
  (`/songbooks/{id}/export-songbook.pdf?keys=songbook`: each song
  transposed by its entry's semitones). Then the coach's repertoire (the
  default songbook), each song either *in your songbook* or *Ask for
  it*; then their groups' songbooks and the community's others they may
  open. Never another student's.
- **Ask for it** (migration 0107, `coaching_song_requests`, one row per
  student and song): only a current student, for their own songbook,
  and only a song of the repertoire. Each of the staff gets a
  notification (`song_request`, in their language) that opens the
  Songbooks section on the request; the row says *Asked*. The coach
  *Adds it to their songbook* (with the key picker when the community
  has its key per student on, `coach_student_keys`), or *Not now* (the
  student may ask again). Added, the student is told
  (`song_request_added`); either way the other staff's notifications of
  it are marked read.
- **The coach** (coaching_book.html, coaching_book.go, coach-book.js):
  one songbook, full width, like a band's. *Requests (n)* first, when
  there are some; then the students and groups as a strip, one line
  each (name, songs, next lesson or session, requests waiting), the
  soonest lesson first, each to their page. Then the songs as rows: the
  repertoire's, then any a student or a group has from elsewhere ("not
  in the repertoire"); title, artist, the song's badges, its key, and a
  chip per student or group who has it, in their key ("Zoé · Gm −2"). A
  chip opens its key (− / +, their range with the song's melody over
  it) to save, or takes the song out of their songbook; *+ Assign*
  gives it to another student or group, in a key (the song page's
  coach-keys endpoint, the community's songbook songs). A search over
  titles and artists, a filter by student ("Zoé's songs", "Not
  assigned", remembered in the address as `?who=s:56`), and *Add song*:
  the one Add / Create dialog where the page has it (add-song.js), else
  the repertoire's own page and its dialog.

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

On a student's page, two kinds of notes (migration 0109):

- **Private**: the coach's and the staff's only. *About {student}* (a
  few lines: who they are as a singer, goals) is always private, and so
  is every note written before 0109.
- **Shared**: the student reads them on their page (*Notes from
  {coach}*, the latest first). The student is told: a notification
  (*New note from Chloé*, `teaching_note_shared`), and a line on the
  studio's overview until they open their page.

The coach picks the kind when writing (private unless she says
otherwise), and may tie a note to a lesson and a song. A private note
can be shared later (*Share with {student}*, asked first); a shared
note can be rewritten or deleted but never made private again: the
student may have read it. A student never gets a private note: not on
their page, not through the API, not in a site admin's *See as…* of
them, not in search or their calendar feed (neither holds notes).

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
