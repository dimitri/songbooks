# Releasing staging (the `review` branch) to production — plan

Status: **draft for the owner's review** (2026-11-23). Production: app.songbooks.studio
(`/var/www/songbooks`, `DOCKER_CONTEXT=default`, stems worker and Galène). Staging:
songbooks.tapoueh.org runs `review` = every open PR merged (88 open PRs; ~250 commits,
~560 files vs `main`).

## 1. What changes (by area)

- **Ownership model** (#145–#152, #153, #165): one owner per song + public/private;
  The Big Songbook; songbooks public/closed; links vs own copies; one access rule;
  set list items in a table; frozen copies; recordings proxied; open catalogue edits
  with a review queue; search/catalogue badges; Add / Create.
- **Bands** (#141): band songs, no other band's song, catalogue links.
- **Coaching** (#143, #164): student view, the coach's single songbook, requests,
  See as…, voice types/notes/melody range, shared edits for assigned students,
  quick add.
- **Home** (#139), **song page** (#136, #144, #154, #156–#163): header, tap tempo,
  no Stage mode, `lead:`, repeats followed, context gating.
- **Guitar Pro & lessons** (#137, #138, #142, #149, #155, #157): GP next to a chart,
  volumes/chords, GeneralUser GS (Docker build downloads it), lesson page, MP3 first,
  no stems for GP tracks.
- **Older PRs** #65–#135 (menus, player, sync, click track, PDFs, …) — see the review
  test plan (docs.tapoueh.org/songbooks-review-test-plan).

## 2. Database migrations (applied on staging, not on production)

0088 0093 0094 0097 0098 0099 0100 0101 0102 0106 0107 0108 0109 0110 0111 0112
**0113 0114** (ownership: drops `variant_visibility`, `setlists.body` via 0115,
`songbooks.shared`) **0115** (set list items: data move — fixed parser, see the
staging incident) 0116 0117 0118 0119 **0123** (stem trigger) 0125.
0126 (melody sheet) is in progress. Migrations apply by set at start-up (order
gaps are fine). **0113–0115 rewrite data: take a backup first** (the rollout does).

## 3. Data steps after deploy (production needs what staging got by hand)

1. `cmd/ownership` dry run → check the counts → `--apply` (catalogue to The Big
   Songbook, band copies to links, What's Up/Koalition to the catalogue, The Final
   Countdown merge, Chloé's charts into the catalogue, Florent's songbooks closed).
   Production's data differs from the staging copy (taken earlier): **the dry run
   must be read, not assumed**.
2. The hand-made staging fixes — decide per item whether production needs them:
   My songbook rebuild (dedupe, coaching keys −2/−3 then removed, Mercy copies
   244 deleted, 242/243/245/249 back in My songbook); Other Lies copies 246–250
   deleted; Saturday Night's Other Lies line removed; Chloé's repertoire and student
   songbook reduced to the 6(+1) songs. Scripts are in the session scratchpad
   (`my-songbook-rebuild*.sql`) — they'd need re-checking against production ids.
3. Not needed on production: `bandsongs` (removed), the set list repair (0115 is
   fixed).

## 4. What needs the owner's own review (not delegable)

- **Security**: the event-manager hole on `main` (any user can make themselves
  manager of any event) — still open; fix before or with the release.
- **Product decisions** listed as open in the test plan and in recent reports
  (catalogue review thresholds, identity = title–artist–reference, "arrangement"
  wording, lessons' rules, coaching student rights on the repertoire).
- **Data**: the ownership dry run on production; which staging hand fixes apply.
- **Legal/third parties**: lyrics fetched from Ultimate Guitar (quick add);
  GeneralUser GS license (free, recorded in the image); YouTube oEmbed fetches;
  recordings now streamed through the app (bandwidth).
- **Public pages freeze**: the song-format docs page changed (`lead:`): keep it out
  of public pages until the freeze lifts.
- **Walk the test plan** on staging (docs.tapoueh.org/songbooks-review-test-plan),
  at least the blockers and the new areas.

## 5. How to ship

The PRs are stacked (bases on other PRs, several based on `review` itself) and
`review` carries a few review-only commits (FR "Mixer" strings, the search palette's
band version, merge fixes). Merging 88 PRs one by one into `main` would replay the
same conflicts already solved on `review`. Recommended:

1. Freeze `review`; walk the test plan; fix what blocks.
2. Merge `review` into `main` in one reviewed PR ("Release 2026-12: …"), listing
   the PRs it contains; close those PRs as merged by it (or merge the stacked ones
   bottom-up if a per-PR history matters — slower).
3. Production deploy with `app/deploy/rollout.sh` (it backs up the database,
   fast-forwards the host checkout, rebuilds — the image now downloads GeneralUser GS
   at build — restarts, waits for HTTP 200). Not during a live lesson (Galène
   restarts).
4. Run §3's data steps (dry run first), then smoke-test production (login, a song,
   a set list, a lesson, a coaching page, The Big Songbook).
5. Rollback: the rollout's backup + the previous image; 0113–0115 aren't reversible
   in place, so rollback means restoring the backup.
