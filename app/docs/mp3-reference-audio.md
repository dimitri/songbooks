# MP3 reference audio — scope

Status: **scoped, not built**. This replaces the plain `reference:` URL
(currently a YouTube link opened in a new tab) with a real in-app audio
player: waveform, transport controls, and seek points synced to the song's
own section structure. Decisions below are made for this deployment —
**invite-only, friends-only, personal use** — and explicitly revisited if
the project ever opens to the public or runs commercially. See
"Revisit at scale" at the end; read that section before changing who can
reach this deployment.

## 1. Source: downloading from YouTube

**Decision: yes, build the convenience.** The reasoning that settled it:
a user who wants this will use one of the many already-available YouTube-
to-MP3 web tools or CLIs regardless of whether we build it in, then upload
the resulting file through a plain file-input form. Not building the
convenience doesn't prevent the download, it just adds a manual round-trip
— so there's no actual protection gained by leaving it out, only friction
for people we've personally invited.

**Mechanism: `yt-dlp`, self-hosted, not a third-party paid API.**
`yt-dlp` (actively maintained fork of `youtube-dl`) runs as a CLI inside
the app's own container, same pattern as the `lilypond`/`typst` binaries
already shelled out to for PDF export. Given a YouTube URL:
```
yt-dlp -x --audio-format mp3 --audio-quality 0 -o '%(id)s.%(ext)s' <url>
```
extracts audio, transcodes to MP3, and the app stores the resulting file
(see §2). No third-party download *service* (an API we'd pay for or
depend on staying up) is in the loop — just a binary we run ourselves,
same operational shape as the rest of this deployment's PDF pipeline.

**The legal reality, stated plainly, not glossed over:** downloading audio
from YouTube violates YouTube's Terms of Service, regardless of which tool
does it. This is **not** a gray area the tooling choice changes. The
reasoning for proceeding anyway: this deployment is invite-only, used by
people the operator personally knows, for personal rehearsal/reference
use, not redistribution — the same informal risk posture already accepted
elsewhere in this deployment (e.g. `SONGBOOKS_DEV_AUTO_LOGIN` enabled only
on this non-production host, at the user's explicit instruction). It is
**not** acceptable reasoning for a commercially operated or publicly
reachable instance — see "Revisit at scale."

## 2. Storage: Postgres, as `bytea`

**Decision: store the MP3 bytes directly in Postgres**, in a new table,
not on a filesystem volume or object storage. Reasoning:

- **One backup covers everything.** `deploy/backup-db.sh` (added this
  session) already `pg_dump`s the whole database. Audio-in-Postgres means
  that one script, already in place, already run, already restorable via
  `restore-db.sh`, covers reference audio too — no second backup target,
  no filesystem volume to separately snapshot and keep in sync with the
  DB's own point-in-time state.
- **No new deploy-time dependency.** No object storage account, no bucket
  policy, no new environment variable, no new failure mode ("DB is up but
  the audio volume didn't mount"). Fits a single-host `docker compose`
  deployment with one named volume, same shape as everything else here.
- **Scale fits.** A friends-only catalog is realistically dozens to a few
  hundred songs. An MP3 at a reasonable bitrate (128–192kbps) for a 4-
  minute song is roughly 4–8MB; even 300 songs is under 2.5GB — trivial
  for Postgres to hold and for `pg_dump`/`pg_restore` to move around
  (the first DB backup taken this session was 180KB with zero audio in
  it yet; headroom is not a near-term concern).

```sql
CREATE TABLE song_audio (
  id            bigserial PRIMARY KEY,
  variant_id    bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  content_type  text NOT NULL DEFAULT 'audio/mpeg',
  byte_size     bigint NOT NULL,
  sha256        text NOT NULL,              -- de-dupe / integrity check
  source_url    text,                       -- the YouTube URL, if downloaded from one
  data          bytea NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  created_by_person_id bigint REFERENCES people(id)
);
CREATE UNIQUE INDEX song_audio_variant_idx ON song_audio(variant_id);
-- one reference track per song variant for v1 — a "pick a different
-- take" flow is a real future feature, not needed to ship this
```

Serving: an HTTP handler reads `data` and writes it with `Content-Type`,
`Content-Length`, and `Accept-Ranges: bytes` — Go's
`http.ServeContent(w, r, name, modTime, bytes.NewReader(data))` already
implements correct Range-request handling (needed for the player's
seek/scrub to not re-download the whole file every time), so no extra
streaming library is needed.

**Named deliberately: this is the thing to revisit first if usage grows**
— see "Revisit at scale."

## 3. Player: custom, no new CDN dependency

Matches this codebase's existing posture — no bundler, no CDN scripts
anywhere in `internal/webassets/static/js/`, every visual element
(simile glyphs, repeat dots, the metronome's Web Audio scheduler in
`clock.js`) is hand-built rather than pulled from a library. Same
approach here instead of adding `wavesurfer.js` or similar:

- **Waveform**: fetch the audio once, decode it client-side with the Web
  Audio API's `AudioContext.decodeAudioData`, downsample the PCM to a
  fixed number of peak buckets (e.g. one per 2–3 horizontal pixels), draw
  with `<canvas>`. Same API family `clock.js`'s metronome scheduler
  already uses — no new browser API surface for this codebase to learn.
- **Transport**: a plain `<audio>` element as the actual playback engine
  (gets scrubbing, buffering, and format decoding for free from the
  browser) with a custom control bar on top: play/pause, stop (pause +
  seek to 0 — distinct from pause, matching the request), restart
  (seek to 0, keep playing if already playing), ±10s (`audio.currentTime
  += / -= 10`, clamped to `[0, duration]`).
- **Click-to-seek on the waveform**: translate a click's x-position to
  `audio.currentTime` via the canvas width ↔ duration ratio.

## 4. Sync markers — "have the structure handy with the player"

A marker is a timestamp tied to one of the song's own sections (the same
`song.Section` list already parsed from the `.song` grammar — no new
structure concept, just timestamps attached to the existing one).

```sql
CREATE TABLE song_audio_markers (
  id              bigserial PRIMARY KEY,
  song_audio_id   bigint NOT NULL REFERENCES song_audio(id) ON DELETE CASCADE,
  section_index   int NOT NULL,   -- index into the song's own Sections slice
  time_seconds    numeric NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX song_audio_markers_unique ON song_audio_markers(song_audio_id, section_index);
```

UI: a structure rail next to the player (same section list the chart
editor already renders as chips — reuse, don't rebuild), each row showing
its label (`Verse 1`, `Chorus`, `Solo`…). Tapping a row seeks the audio to
that marker's `time_seconds`; during playback, the row whose marker is
most recently passed highlights, same "track playback position against a
structure list" pattern `clock.js`'s bar/section highlighting already
implements for the metronome, just driven by `audio.currentTime` instead
of a BPM-derived clock. **Setting** a marker: play/scrub to the right
spot, tap "set marker here" next to a section row — writes one row via a
small `POST /api/songs/{book}/{stem}/audio/markers` call. No automatic
beat-detection/alignment in v1 — a human sets markers by ear, which is
both simpler to build and more reliable than audio analysis for a rock/
pop reference track with no guaranteed clean transients.

## 5. Build order

1. `song_audio`/`song_audio_markers` tables + migration.
2. Upload: a plain file-input form (works today, no YouTube dependency) —
   `POST` a chosen MP3 file, store it, basic validation (content-type,
   a sane size ceiling, e.g. 50MB).
3. Player: waveform canvas + transport controls + Range-request serving.
4. Markers: set/seek/highlight, reusing the existing section-chip UI.
5. YouTube convenience: `yt-dlp` wired into the upload flow as a second
   path ("paste a YouTube URL" next to "choose a file") — deliberately
   last, since 2–4 deliver real value and are a strict subset of 5's
   legal exposure; shipping them first means the whole feature isn't
   blocked on settling how comfortable this deployment is with 5's risk.

## Revisit at scale

Flip any of these — public signup, a paid tier, a larger/stranger user
base, or anyone treating this as more than a favor for friends — and the
calculus above needs re-deciding, not just re-confirming:

- **Drop the YouTube download convenience**, or gate it hard behind an
  explicit per-user "I confirm I have the rights to this audio" step with
  real consequences for lying, not a one-time friends-only shrug. At
  minimum, stop shipping it server-side; a client-side "paste a URL you
  already downloaded elsewhere" is a materially different risk profile
  than this deployment running the extraction itself.
- **Move audio out of Postgres into object storage** (S3/R2/similar) with
  signed, time-limited URLs served via a CDN. `bytea` storage doesn't
  streaming-scale: every request round-trips through the app process and
  the database, `pg_dump` grows linearly with every byte of audio ever
  uploaded (no incremental audio-only backup), and Postgres's own MVCC/
  TOAST machinery isn't designed for the write/read pattern a large binary
  blob table produces at real traffic.
  **Sketch, not yet scoped in detail:** keep `song_audio`'s metadata row
  (variant_id, content_type, byte_size, sha256, source_url) in Postgres —
  it's small and relational — but replace the `bytea` column with an
  `object_key` pointing at a bucket (Cloudflare R2 is the natural pick for
  a small/self-hosted deployment: S3-compatible API, no egress fee, which
  matters once reference audio is actually being streamed by listeners
  rather than just stored). The app mints a short-lived presigned GET URL
  per request instead of serving bytes itself; upload (both the yt-dlp
  path and a direct file-input) streams straight to the bucket via a
  presigned PUT, so the app process never holds a whole MP3 in memory.
  Migration path: a one-off script reads every existing `song_audio.data`
  row, PUTs it to the bucket, records the `object_key`, then the column
  drops — straightforward since every row already carries its own sha256
  for post-migration integrity verification. This becomes worth doing
  once either (a) total audio size meaningfully slows `pg_dump`/
  `pg_restore`, or (b) the jam-video-upload idea (see
  `docs/jam-videos-and-paid-features.md`) ships, since video files are
  10–100x an MP3's size and make the `bytea` approach's weaknesses acute
  immediately rather than gradually.
- **A real rights/takedown process.** Even self-hosted, a public or
  commercial instance needs a DMCA contact, a takedown workflow, and
  almost certainly a licensing conversation (sync licenses, mechanical
  licenses) that an invite-only personal tool never triggers in practice.
- **Storage-size accounting becomes a real cost line**, not a rounding
  error — budget per-user storage quotas before this matters, not after.
