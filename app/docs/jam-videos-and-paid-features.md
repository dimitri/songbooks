# Jam videos, event review pages, moderation, and paid features — scope

Status: **explored, not built**. Captures the shape of a feature cluster
raised together because they share infrastructure (object storage,
per-event pages) and a framing (moderation + video conferencing as the
first paid-tier features, everything else in this app stays free). Read
`docs/mp3-reference-audio.md` first — object storage is a shared
prerequisite, not duplicated here.

## 1. Phone video upload during a jam, assigned to a song

**The shape of it:** someone at the jam, phone in hand, records a clip of
a song being played, uploads it from the jam's own page, and assigns it to
whichever song/slot in the jam's setlist it was. This only makes sense
once the event model exists (see the architecture discussion elsewhere
this session — jam becomes one event type a community hosts), since
"assign a video to the jam song in the setlist" presumes the jam has a
structured, ordered song list the upload can point at — which today's
`jam_songs`/`jam_picks` tables already give it, so this doesn't block on
the bigger event-model work, just benefits from it.

- **Upload mechanism:** a plain `<input type="file" accept="video/*"
  capture="environment">` on the jam's live page — no custom camera UI,
  the phone's own camera app handles capture, this just needs the
  resulting file. Direct-to-object-storage presigned PUT (see object
  storage below) rather than routing the video through the app server,
  since a phone video can run 50–500MB+ and the app process shouldn't
  buffer that.
- **Assignment:** a dropdown/picker of "which song was this" scoped to
  the jam's own song list (`jam_songs`), defaulting to whatever's
  currently the jam's "called" song (`jam_events.called_song_id` already
  exists) at upload time — the common case is uploading right after
  playing, so the right default needs no picking at all.
- **Who can upload:** any current `jam_profiles` row for that event
  (anyone who joined the jam, whether logged-in or anonymous-nickname) —
  this is the one place an anonymous jam participant would need write
  access to something that outlives the event, which is new; today
  `jam_profiles` only ever read/signed-up, never uploaded durable
  content. Needs its own thought: does an anonymous uploader's video
  survive if they never claim an account? (Lean: yes, tied to the
  `jam_profiles` row and the event, not to a Person — same "it's the
  event's data" framing as `jam_picks`.)

## 2. Event review page — "view all the jam videos after the fact"

A per-event page, reachable after the jam has ended (not just while
`jam_events.expires_at` is still in the future — this is explicitly a
*look back* page, the opposite lifecycle from the live signup/play
page), showing: the setlist as it was actually played (`jam_picks`
already records call order), and every uploaded video grouped under the
song it was assigned to. This is a natural fit for the "community home
page" (events + songbooks + members) raised in the same architecture
discussion — the event review page is what a past event in that listing
links to.

Access: scoped to whoever was *at* the jam (has a `jam_profiles` row for
that event) plus community staff — not public by default, since a phone
video of someone playing is a meaningfully more sensitive thing to leave
world-readable than a chord chart.

## 3. Moderation options for community staff

Once anonymous/semi-anonymous people can upload video that other members
see, staff need a way to take one down — this is the direct consequence
of §1/§2, not a separate feature idea. Minimum viable set:

- **Remove a video** (soft-delete, same history-preserving convention as
  `band_members`/`school_members` — mark it removed, don't destroy the
  row, so there's a record staff acted rather than silent disappearance).
- **Report** — a logged-in member flags a video; staff see a short queue,
  same shape as `school_submissions`' pending/accept/reject pattern
  already built for song-submission review, reusable as a template.
- **Per-event upload toggle** — staff can close uploads for a given
  event entirely (the "going to a show together" / public-facing event
  types especially want this off by default, probably).

This is real moderation-queue UI work (a staff-only review surface,
notification-on-report), not a trivial add — scope it as its own phase
when the time comes, not folded silently into the upload feature itself.

## 4. Paid-tier framing

Two features were specifically called out as the first candidates for a
paid tier, as opposed to everything else in this app staying free:

- **Moderation tooling** (§3) — the reasoning: moderation is ongoing
  operational work (reviewing reports, storage for removed-but-retained
  content, staff tooling upkeep) in a way a one-time feature isn't, so
  it's a natural place to ask a community that wants it to help cover
  that cost.
- **Galène integration** (§5) — a hosted video room is a recurring
  infra cost (bandwidth, a running SFU process) unlike everything else
  in this app, which is static content plus Postgres.

Neither needs a payment/billing system designed now — noting the framing
so the eventual gating point (a community's `plan` or `tier` column,
checked before exposing the moderation panel / the "add a Galène room"
button) is remembered when the event model's schema is actually drafted,
rather than retrofitted.

## 5. Galène for online-chat events

[Galène](https://galene.org) (Juliusz Chroboczek, OSS, AGPL) is a
lightweight SFU (selective forwarding unit — relays media, doesn't
transcode) built specifically for exactly this use case: small-group
video chat, self-hostable, no account system of its own beyond simple
room passwords. Two integration depths, from least to most work:

- **v1 — plain link-out (this is the only one scoped for now, per this
  session's own decision to park Galène as a todo, not build it yet).**
  An `online_chat` event stores a `video_room_url` — either a URL into a
  Galène instance this deployment's operator already runs, or a
  manually-pasted link to any other video tool (Jitsi, a Google Meet
  link, whatever the community actually uses). The app does nothing
  Galène-specific: just a "Join video chat" button rendering that URL.
  Zero new infrastructure, zero new credentials, works even if Galène is
  never self-hosted at all.
- **v2 — provisioned rooms.** Galène's own admin surface
  (`galene-cli`/its group-management "groups" API, configured via a
  `group.json` per room, or its REST-ish admin endpoints in newer
  versions) lets an operator create/configure a room programmatically:
  name, password, recording on/off, max occupancy. This app would call
  that at event-creation time (mint a room scoped to the event, matching
  the event's own id/slug) and tear it down after. Needs: a Galène
  instance reachable from the app server, admin credentials stored as a
  deploy secret (same pattern as `SONGBOOKS_GENIUS_TOKEN`/SMTP
  credentials already use), and someone running/maintaining that Galène
  instance — this is a real new piece of self-hosted infrastructure, not
  a dependency-free API call, so it's the natural thing to gate behind
  the paid tier (§4) once built.

Running Galène itself is cheap (a single Go binary, no database, modest
RAM for a small jam-sized room) — the cost that makes this a paid-tier
candidate is ongoing operational attention and bandwidth, not software
licensing (AGPL, free).
