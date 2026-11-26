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

### Automatic (2026-11-26)

The song's MP3 is what the click, the sync, the parts (the stems split)
and the melody start from, so it's no longer a button: it's fetched by
itself, in the background (`httpapi/yt_audio_jobs.go`,
`store/youtube_audio_jobs.go`, migration 0140).

- **One job per song** (`song_youtube_audio_jobs`, keyed by the song, for
  one link): `queued` → `running` (progress and ETA read from yt-dlp's
  `--newline` progress lines, a heartbeat every 10 s) → `done` (stored as
  an upload is: `song_audio`, category `youtube`, whose triggers queue the
  split, then the analysis, then the melody) or `failed` with a reason:
  `age_restricted`, `unavailable` (private, removed, members only),
  `blocked` (a bot check, 429/403, a country), `too_long` (over 30 min),
  `other`. Rate limits and network errors are tried again after 1, 5,
  then 20 min (4 tries in all); the others fail at once. `removed`: an
  editor removed the audio, it isn't fetched again for that link until
  someone asks.
- **When it starts:** the songs trigger queues it whenever a reference is
  set or changed to a YouTube video, whatever wrote it (the editor, the
  YouTube picker, quick add, The Big Songbook's fixes and new recordings,
  proposals, imports); a link put back to one whose audio is there, or
  taken away, drops the other job. The song page queues it when the song
  is opened without its audio, by anyone who may open it, unless it
  failed in the last 24 h or an editor removed it. An editor's ↻ Try
  again / Again (`POST /api/songs/{id}/audio/youtube`) queues it whatever
  came before; the import wizard's Recording step posts the video it
  picked and follows the job.
- **Workers:** in the app process, `SONGBOOKS_YTAUDIO_WORKERS` at a time
  (1 by default, at most 2, 0 for none), woken by `NOTIFY
  song_youtube_audio`, looking every 30 s anyway. At start-up the jobs a
  worker was on are queued again; a job whose worker went quiet for 3 min
  too. `SONGBOOKS_YTDLP` points at another yt-dlp (a local stand-in).
- **Under the video** (the Reference view; the lesson view's video when
  it's the song's): "Getting the audio…", a progress bar, the % and the
  time left, "You can leave this page"; "✓ Audio ready · 3:42" (Again ·
  Remove for an editor); the reason in one line (↻ Try again and ⬆ Upload
  an MP3 for an editor, the reason alone for anyone else). `GET
  /api/songs/{id}/audio/youtube/status` is asked again while it's queued
  or running (paused while the page is hidden); once ready, the player
  takes the audio without a reload.
- **The songs there already:** `backfillaudio` counts (a dry run by
  default); `--apply` queues them one at a time, waiting for each, with a
  pause between.

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

### Sync state: estimated or set

Three things place a chart on a recording, and the player says which one
it follows (`audio-player.js`, "Sync state"):

| Where it comes from | Saved? | State |
|---|---|---|
| The player's own detection (no markers, no sync points: `mixer-autoalign.js` run in the browser) | no | estimated |
| A program's proposal: the import wizard's step 5 (`songimport.ApplySync`), the Mixer's Auto-align | yes, `origin = 'auto'` | estimated |
| A person: a tap or a nudge in the Sync sheet, a flag dragged in the Mixer, a Guitar Pro file's own sync, "It's right: confirm" | yes, `origin = 'set'` | set |

`song_audio_markers.origin` and `gp_bar_sync_point.origin` (migration
0094) hold it; a recording is "estimated" while any of its markers is. The
badge by the counter reads **Synchro estimée** or **Synchro réglée**; its
panel says where the sync comes from and opens the Sync sheet to correct
it.

The badge also warns when the markers make the bar.beat counter run at
another tempo than the chart's (more than 8% off): the chart's bars are
spread evenly between two section starts, so a section with more bars
written than the recording plays makes the counter run fast. Song 334 was
the case: its verses are written with 23 bars and sung over 17 (the
recording is at 90 bpm, the chart says 104), the wizard placed each
section where the singing starts, and the counter ran at 122 bpm. The
wizard now says so too, section by section ("23 bars in the chart, about
17 in the recording") and keeps the chart's tempo rather than proposing
the 122 its fit implies (`songimport.ProposeSync`, `TempoMismatch`).

Only the starts set by ear count for that warning: a span between two
section starts is compared with the chart's tempo only when both ends are
`origin = 'set'`. A span touching an estimate never warns — not the
badge, not the panel, not the Sync sheet's box or its rows — and never
feeds the one-click "Use ♩. = N as the chart's tempo": estimates may be a
section off. Wake Up Alone (song 335) was the case: six unchecked
estimates made the counter run at ♩. = 46, while the chart's ♩. = 72
(tapped on 1 and 4 of its 6/8 bars) was right, and the one-click fix would
have broken it. The Sync sheet still shows an estimated span's tempo on its
row, muted and marked "≈" (`sync-wave-math.js` `spanByEar`, `offSpans`,
`medianTempo(…, { byEar: true })`, `tempoFix`; `audio-player.js`
`syncTempos`).

The panel's Sync line is always there with a recording, for everyone:
its state ("set by ear", "n to check", "estimated", or "the Guitar Pro
file's"), where the sync comes from, and for an editor the Sync sheet
("Correct the sync", or "Review or adjust…" once it's all set).

### The Sync sheet

`sync-sheet.js`, for editors: tap along while listening. Play, tap the
big button on the first beat of each section; the tap sets that section
and moves on, never seeking. A tap snaps to the nearest detected downbeat
or to the voice coming in (`GET /api/songs/{id}/audio/{category}/voice`:
the vocals part's phrases) within 0.15 s; the delay left after snapping
is learnt and taken off later taps. Each section has ▶ 2 s before, ±0.1 s,
±1 bar, ✓ (right as it is) and "Redo from here". The chart, the counter
and the click follow the unsaved sync while it's edited. Undo goes back
step by step; Save writes the whole sync in one request
(`PUT /api/songs/{id}/audio/{category}/markers`), which keeps the sync it
replaces (`song_audio_sync_history`, the last 20,
`GET …/markers/history`) so an earlier one can be taken back.

It opens from the badge (an editor's tap), the panel's Sync line, the
song's ⋯ menu and the phone's Morceau sheet ("Sync…", for whoever may
edit the song, whenever it has a recording: a sync set by ear can always
be reviewed and adjusted), the Mixer, and the import wizard's step 5
("Adjust by ear…", with the wizard's proposal). A Guitar Pro file's own
sync opens there read-only, as before.

Its top is the recording's waveform (`sync-wave.js`, its arithmetic in
`sync-wave-math.js`, tested by `internal/webassets/jstest`), drawn on a
canvas from peaks at several resolutions (built from the player's ~11 kHz
mono copy), zoomed from the whole song to a few beats: trackpad pinch,
Ctrl/⌘ + wheel, a phone's two fingers, − / Fit / + (keys − 0 +). The
wheel or a drag scrolls, a click plays from there, and while playing it
follows the playhead until you scroll or zoom (Follow, F, brings it back);
an overview strip under it shows the window and moves it. On it: the
sections as a band, the bar lines with their numbers, the chart's chords
(`GET /api/songs/{id}/mixer`), the analysis' beats and downbeats faintly,
its suggestions as dashed lines with a chip (once: that section; twice:
taken), its chord checks as flags (they open the analysis there). Each
section's start is a marker to drag, snapping to the nearest downbeat,
beat or voice onset within a few pixels (`Alt` places it freely), the bars
between the starts following. Below it: the section the next tap sets
with its tools, the list of sections (a dot for each one's state, notes
for what needs a look), and the analysis. A phone stacks them, the
waveform on top and Save at the bottom.

### Lines in Lyrics only

A chart has no time per lyric line. While the recording leads, Lyrics
only lights the line the singer is on from the vocals part's phrases
(`lyric-lines.js`): the phrases starting in the section's span are merged
(shortest rest first) or split (longest phrase first) until there is one
per line, and each line starts with its phrase. Without the voice (the
recording not split yet, or the metronome leading), the section's lines
are spread evenly over its bars, as before.

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
