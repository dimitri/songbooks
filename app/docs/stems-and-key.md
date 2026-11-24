# A recording's key and its parts

What the practice player and the Mixer do with a song's reference
recordings (a YouTube download, an uploaded MP3, a Guitar Pro file's own
recording): play them in another key, and play them without the voice,
or with only some of their parts. Not the band's recordings on pCloud:
pCloud's links refuse Web Audio (no CORS), and splitting them would mean
copying them here.

## Key

*Key* in the practice player (audio-player.js) moves the recording up or
down by semitones, ±7, as it plays. The chords on screen follow: the
song's own −/+ (song.js, view-only) is clicked as many times. The other
way round, the song's −/+ alone doesn't move the recording: it's also a
capo's arithmetic. The key is kept per song in this browser, with the
other practice settings.

- [Signalsmith Stretch](https://signalsmith-audio.co.uk/code/stretch/)
  (MIT, `static/js/vendor/signalsmith-stretch`, its official WASM and
  AudioWorklet build), loaded the first time a key is changed. Live-input
  mode: `<audio>` → `MediaElementAudioSourceNode` → the shifter →
  speakers; the `<audio>` keeps the speed (`preservesPitch`), the shifter
  the pitch, with formant compensation (a voice stays a voice).
- The click and the count-in wait for the shifter's latency.
- Up to 2–3 semitones it sounds natural; the panel says so beyond 3.
- Web Audio only once a key or the voice is used: until then the plain
  `<audio>`, which iOS keeps playing from the lock screen. Changed, it may
  stop when the phone locks (iOS suspends a page's AudioContext).

## Parts (stems)

Every reference recording is split in the background into its parts:
vocals, drums, bass, other, and no_vocals (the three others summed).

- **Queue**: `song_audio_stem_jobs` (migration 0078). A trigger on
  `song_audio` queues a recording when it's added or replaced, whatever
  wrote it (upload, YouTube download, Guitar Pro import, songimport), and
  notifies `song_audio_stems`; the recordings already there were queued by
  the migration. An editor can queue one again
  (`POST /api/songs/{id}/audio/{category}/stems`).
- **Worker**: `cmd/stemsd`, in its own container (`deploy/Dockerfile.stems`:
  Python 3.11, PyTorch 2.5.1 CPU, Demucs 4.0.1, the `htdemucs` model built
  in; about 1.8 GB). It takes the oldest job (`FOR UPDATE SKIP LOCKED`),
  converts the recording to WAV, runs Demucs, encodes the parts to 128
  kbit/s MP3 with ffmpeg, mixes no_vocals, stores them. A recording
  replaced while it was split: the parts are dropped, the new one is
  queued. Three failures and it stays failed, with the error. Capped at 8
  cores and 8 GB in docker-compose.yml.
- **Measured** (4 threads on a laptop): a 2:49 song in 1 min 20 s, 13 MB of
  parts; a Guitar Pro recording in 21 s.
- **Served** at `/song/{id}/audio.mp3?category=…&stem=vocals`, to who may
  see the song, never as a download link; `GET /api/songs/{id}/audio`
  says which parts there are and where the split is (`stems`,
  `stemStatus`).

### My parts ("Je joue"), in the practice player

Shown once the parts are there (the player checks every 20 s while a split
is queued or running, with a greyed *Voix · séparation…* chip meanwhile;
failed, *Voix indisponible* and a retry for editors). The player shows one
level per part the reader plays in this context — 0 takes it out of the
recording, 100 leaves it — as chips in its bar (a tap takes the part out
and back, V for the voice) and in its panel's *Son* group, which also
picks the instruments.

Which instruments (`store/player_parts.go`, migration 0100
`player_parts`): the reader's choice for this song in this context (a
band, a community, or none), else their choice for the context, else
their profile's instruments (`people.instruments`, normalised by
`internal/instruments`), else the voice. Saved for the reader only
(`PUT /api/songs/{id}/parts`). An instrument is heard in one Demucs part:
voice in vocals, bass and double bass in bass, drums and percussion in
drums, everything else (guitar, keys, horns…) in other; two in the same
part share one level, and the panel says the split can't tell them apart.

How it plays: the recording itself when every level is 100; with only the
voice down, no_vocals in place of the recording (same length: the
position carries over) and the vocals beside it at their level; with any
other part down, the four parts side by side, each through its own gain.
The parts are kept within 80 ms of the one that drives the clock.

### A coaching community's player option

*Key per student* (`communities.coach_student_keys`, migration 0097), its
staff's to turn on or off on its admin page, for a coaching community
only, on for Chloé Paul's coaching community only (id 7; Studio Lumen on
the demo copy). `store/vocal_slider.go`. In the songs opened from the
community's songbooks (a student's, a group's, its repertoire), its page
or one of its events (`songCoachingContext`), the coach picks a student
and saves their key from the panel's Key line. The list is that
community's students and groups, who has a lesson in the coming 7 days
first, the soonest first, its date said, then the others folded under
*Other students* (`/api/songs/{id}/coach-keys?community=N`,
coach_keys.go). A name picked, the key goes to theirs, *Save for Poppy*
keeps it; opened from a student's songbook, that student is picked.
Elsewhere, or with it off, the Key line lists everyone the coach coaches
for the song, by name.

The voice slider used to be a second option of this kind
(`communities.player_vocal_slider`, migration 0093); it no longer gates
anything — everyone has their parts' levels — and the column stays,
unused.

A student's key (*Your key: C#m*) is for the songs they practise in the
coaching community: opened from their songbook there, its repertoire, a
group's list, an assignment, an event. Opened from a band, a set list or
anywhere else, the song is in its own key and says nothing of theirs. On
the key's chip they see their key (*la vôtre*), may try others without
saving (*Back to my key*).

The player's bar and panel (`app/reports/player-proposal.md`) lay these
out: the tools on the bar's line by priority — those that are off leave
it from the right into "+N", those that are on never do
(`PlayerShell.fit`, `player-shell.js`) — and their settings on their
lines in the panel.

### Lanes, in the Mixer

A split recording has its four parts as lanes under it (Vocals, Drums,
Bass, Other), each with mute, solo and volume, playing in the recording's
time. The recording itself stays the clock and its lane, muted to begin
with: the parts add up to it. Each person's mix is kept like the rest of
the Mixer's.

## Where the files live: Postgres for now

Audio is `bytea` in Postgres since the start (docs/mp3-reference-audio.md),
like the download areas' files (`file_blobs`). In production today: 85
recordings, 332 MB, a 711 MB database. The parts add about four times the
recordings' length at 128 kbit/s: about 1 GB more.

That's still small for Postgres: the cost is in the backups (pg_dump, and
`local-restore.sh` copying production's dump to a laptop), and in serving
(the whole value is read for a request; a part is 2–6 MB).

MinIO isn't the answer any more: its community edition lost its console
(May 2025), its published images (autumn 2025), went into maintenance
(December 2025) and was archived (February 2026).

When to move, and where:

- **The trigger is students' recordings** (the next feature set): many
  files, video among them, written all day. Then the media go out of the
  database, all at once (`song_audio.data`, the parts, `file_blobs`, the
  recordings), keyed by their SHA-256, and the rows keep the metadata.
- **First step: a directory on the host** (a Docker volume), files named
  by hash, served by the app with `http.ServeContent` from the file
  (Range, no memory), backed up by rsync beside pg_dump. No new service.
- **Object storage when it has to scale or go elsewhere** (several hosts, a
  CDN, offloading the server's bandwidth): an S3-compatible store behind
  one small interface. Self-hosted, [Garage](https://garagehq.deuxfleurs.fr/)
  (made in France, light, AGPL) or SeaweedFS (Apache 2); hosted in the EU,
  Scaleway, OVHcloud or Hetzner Object Storage, with signed URLs for
  playback.

Sources on MinIO: [linuxiac](https://linuxiac.com/minio-ends-active-development/),
[It's FOSS](https://itsfoss.com/news/minio-moves-away-from-open-source/),
[TuxCare](https://tuxcare.com/blog/minio-els/).
