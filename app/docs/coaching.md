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

## Lessons

`/communities/{id}/lessons`: the coach's calendar, day by day; a lesson
(an event of type `lesson`: when, how long, where, a name) and its
students, registered by the coach (the registrations are theirs to make,
for now). A student sees their own lessons there, on their page, and on
their home calendar; the coach sees them all. Lessons stay off the
community's Events (and their pages, and its overview), which keep
everything else: concerts, jams, meetups… and now open mics (event type
`open_mic`, for any community or band).

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

## Next: warm-ups

Not automated: the coach writes and sings them.

- **An exercise** is a song of the community with a Guitar Pro file: a
  piano track, written in the score editor that exists (gp_edit), with
  the syllables or words to sing under its notes (the file's lyrics), and
  notes to read beside it. Its starting key is the one it's written in.
- **The coach's voice**: *Record my demo* plays the exercise and records
  the coach's microphone over it in the browser (raw microphone, as the
  tuner does), kept as the exercise's recording, lined up with its bars.
- **The Mixer** has its two lanes, the piano (MIDI) and the coach's voice,
  each with its volume, mute and solo.
- **A student's practice range**, per exercise, set by the coach: a path of
  keys, as semitones from the written key, through any waypoints (start
  mid-range, up first, then down past the start, up again…), a semitone
  a repeat. The practice player walks it: each repeat the piano (alphaTab
  transposes its playback) and the coach's voice (the Key's pitch
  shifter) move together.

## Review: live lessons with Galène, inside the app

Not built; what it would take.

[Galène](https://galene.org/) (MIT, written in Go by Juliusz Chroboczek)
is a videoconference server: an SFU, one binary, groups described in
JSON files, a built-in TURN server, a web client, and a documented
protocol with a JavaScript library to build one's own client
([README.PROTOCOL](https://github.com/jech/galene)).

- **Fit**: light enough to sit in our compose file beside the app (a 1:1
  lesson at 720p is about 1.5 Mbit/s each way); Go, as we are; no user
  database of its own: an *authorization server* signs tokens it trusts
  ([design](https://lists.galene.org/galene/87r1c3wevd.wl-jch@irif.fr/T/)).
  The app would be that server: a lesson's student and coach get a token
  for that lesson's group, nobody else, no second login. Since 2025 a
  token can cover a group and its subgroups: one group per community, a
  subgroup per lesson. Recording is per group (`allow-recording`): the
  coach records, the file comes back to the lesson.
- **The sound of a voice**: the SFU forwards what the browser sends, so
  quality is the client's to ask for: the raw microphone (no echo
  cancellation, no noise suppression, no gain control: they wreck a
  singing voice, as they did the tuner), Opus in stereo at 96–128 kbit/s.
  Headphones then, or the echo comes back.
- **Latency**: WebRTC is about 100–300 ms mouth to ear. A lesson's talk,
  demonstration and call-and-response are fine; the coach accompanying
  the student live, in time, is not (that's Jamulus or JackTrip territory,
  under 30 ms, same region only). What works instead: the student sings
  over the backing playing on their side (our player, in their key), the
  backing mixed into what they send, so the coach hears both in time.
- **Inside the app**: a lesson on the calendar gets *Join*: a page of ours
  with the video tiles (our client on Galène's protocol) beside the song
  or the warm-up being worked on (score, key, voice), the coach able to
  send the student to a song.
- **Hosting**: TLS for its websocket behind our proxy, media over UDP to
  the host directly (a port, or its TURN on 1194): firewall rules to open
  on the server.
- **Others**: LiveKit (Go, Apache 2, more SDKs, more to run), Jitsi
  (heavier, Java), mediasoup (a library, everything else to write),
  BigBlueButton (a classroom suite). Galène is the lightest and the
  closest to how we work; its catch is a smaller community and one
  client library (JavaScript), which is all we need.
- **Effort**: hosting, tokens and the Join page, 2–3 days; the singing
  sound and the backing mixed in, 2–3 days; recordings to the lesson,
  1–2 days.
