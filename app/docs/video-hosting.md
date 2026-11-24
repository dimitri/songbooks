# Video lessons and community videos in the app — YouTube or our own hosting?

Status: **notes for later** (2026-11-23), for review. Prices and features below are
from general knowledge and must be checked before deciding.

## The need

Teachers (Florent, Chloé) and communities want to share video inside the app:
lessons, exercises, a rehearsal recap, a community's monthly video — some free,
some **paid** (Florent's methods are sold).

## Options

| | YouTube (unlisted) + our player (today) | Managed video service (Bunny Stream, Cloudflare Stream, Mux…) | Self-hosted PeerTube (or plain HLS on object storage) |
|---|---|---|---|
| Upload | the teacher uploads to their channel, pastes the link | upload in the app (resumable), the service transcodes | upload in the app; we transcode (ffmpeg) or PeerTube does |
| Cost | free | storage + delivered minutes/GB (low for our volumes) | servers, storage, bandwidth, ops time |
| Paywall | **none**: an unlisted link works for anyone who has it; private videos need each viewer's Google account | **signed, expiring URLs** per viewer; per-course access | ours to build (signed URLs) or PeerTube's internal privacy |
| Player | YouTube's iframe (ads/recommendations possible, its branding), our dock drives it (#157) | their player or ours on HLS (hls.js), full control | ours on HLS, full control |
| Reach | public discovery on YouTube (good for teachers' marketing) | none | none (federation, little use here) |
| Risks | YouTube policy changes, Content ID claims on music, teacher's channel dependency | vendor lock-in (mitigated: plain HLS export) | ops burden, bandwidth spikes, transcoding CPU |

## Recommendation

- **Free and marketing content**: keep YouTube links (what Florent already does); our
  player and the lesson layout (#157) are built for it.
- **Paid content and private community videos**: a **managed service with signed
  URLs** (Bunny Stream is the cheapest sensible one; Cloudflare Stream is simple),
  played by our own player on HLS so the dock (Watch / Play along, loops, speed,
  chapters pinned to bars — see `score-follows-video` idea) works the same.
- **Self-hosting (PeerTube)** only if independence from vendors becomes a goal; not
  worth the ops now.

## Before building

Measure volumes (hours of video per teacher, views per month), check current
pricing, decide where uploads happen (app vs service dashboard), and how a teacher
moves an existing YouTube lesson into a paid course.
