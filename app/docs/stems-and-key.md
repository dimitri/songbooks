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

### Voice, in the practice player

Shown once the parts are there (the player checks every 20 s while a split
is queued or running). *Voice* turns the voice off and back on (V); its ▾
sets the volume: Off, Guide (25%), Soft (60%), Full, or anywhere between.
Turned down, the player plays no_vocals in place of the recording (same
length: the position carries over) and the vocals beside it at that
volume, kept within 80 ms of it. Full, the recording itself.

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
