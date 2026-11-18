# songbooks deployment (dev-runner)

This directory is the self-contained deployment artifact for the songbooks
app: a multi-stage `Dockerfile` (Go binary + the `typst`/`lilypond`/
`pdfinfo` CLIs the PDF export routes shell out to) and a `docker-compose.yml`
wiring it to a Postgres 16 container. It's built to run on `dev-runner`
(the shared host already running the gites/pr0/faits-politiques stacks),
reverse-proxied by the host's existing Caddy under `songbooks.tapoueh.org`.

## Layout on the host

```
~/dev/www/songbooks/      <- a git clone of this repo (origin = GitHub),
                              not an rsync'd tree — see "Redeploying" below
  app/
    deploy/.env           <- real secrets, gitignored, never overwritten
                              by a pull
  v2/
```

`docker-compose.yml`'s build context is `..` relative to this `deploy/`
directory, i.e. the `app/` directory itself — so `deploy/` must stay
checked out as a subdirectory of `app/`, same as in this repo.

**Why git, not rsync**: an earlier phase deployed with
`rsync -a --delete app/ dev-runner:.../app/`, which — because `deploy/.env`
only ever exists on the host and is gitignored — silently deleted the
live `.env` on every redeploy, once actually taking the site down
(blank Postgres password, recovered by resetting the role's password).
`git pull` only ever touches tracked files, so a gitignored `.env` is
never at risk.

## First deploy

1. Clone the repo onto the host (it already has its own GitHub deploy
   key at `~/.ssh/id_ed25519_github`, confirmed working):

   ```
   ssh dev-runner 'git clone git@github.com:dimitri/songbooks.git ~/dev/www/songbooks'
   ```

2. On the host, create `~/dev/www/songbooks/app/deploy/.env` from
   `.env.example` (real `POSTGRES_PASSWORD`, and
   `SONGBOOKS_WEBAUTHN_RP_ID=songbooks.tapoueh.org`). This file is
   gitignored — it is never touched by a later `git pull`.

3. Build and start:

   ```
   cd ~/dev/www/songbooks/app/deploy
   docker compose build
   docker compose up -d
   docker compose ps
   ```

   The `app` container runs the embedded migrations automatically on every
   boot (`internal/store/migrate.go` — idempotent, tracked in
   `schema_migrations`), so there's nothing extra to run for schema setup.

4. **Song content.** This deployment is meant to carry the user's real,
   already-imported song library (projects Mercy/OtherLies/BandsCamp/etc,
   141 songs locally as of this writing) rather than start empty, so the
   first deploy restores a `pg_dump` of the local dev database instead of
   re-running `--import` against the v2/ tree:

   - Local dev DB (`songbooks_dev`) was confirmed fully migrated
     (`schema_migrations` max version 27, matching all 27 embedded
     `.sql` files) before dumping, so the dump's schema matches what the
     deployed binary's embedded migrations expect — no reconciliation
     needed, but the auto-migrate-on-boot step would handle it forward-
     only if a mismatch ever did show up.
   - Dumped **schema + data** (`pg_dump -Fc`, not `--schema-only` and not
     `--data-only`): schema-only would leave an empty, unusable instance;
     data-only would require the target schema to already match exactly,
     which is more fragile than just shipping both together in one dump
     file restored into a freshly-migrated (empty) database.
   - Restore is `pg_restore` into the new container's `songbooks` database
     *before* the app's own migration count can diverge further — in
     practice this just means: start `db` alone, restore, then start `app`
     (see the exact commands run for this deployment in the phase report).

   An alternative for a *future* fresh environment with no existing dump
   (e.g. a disaster-recovery rebuild) is `songbooks --import /path/to/v2`
   against a copy of `v2/` on the host, which walks every book directory
   under it and loads it into an empty database. That path exists and
   works, but was not used for this deployment since a real pg_dump of the
   actual working database was available and preserves more than the
   import path would (notably: user accounts/admin flag, bands, setlists,
   jam/school data — none of which lives in `v2/` at all).

## Redeploying after a code change (manual — no CI/CD in this phase)

1. Commit and push to GitHub, as usual:

   ```
   git push origin main
   ```

2. Run the rollout script from your machine:

   ```
   app/deploy/rollout.sh
   ```

   It backs up the live database, `git fetch`es on the host, refuses to
   proceed if the host's `origin/main` doesn't match what you just pushed,
   fast-forwards the host's checkout (`git merge --ff-only` — this only
   ever touches tracked files, so `deploy/.env` is safe), rebuilds the
   Docker image, restarts the stack, and polls the site until it returns
   HTTP 200 (or reports a failure with the log command to check).

   Equivalent by hand, if you ever need to do it manually:

   ```
   ssh dev-runner 'cd ~/dev/www/songbooks && git pull --ff-only'
   ssh dev-runner 'cd ~/dev/www/songbooks/app/deploy && docker compose build && docker compose up -d'
   ```

`v2/` content (songs, templates) lives in the same repo and pulls down with
the same `git pull` — no separate step. No restart is needed for a `v2/`
content-only change; the app reads it at request time.

## Bringing it down

```
ssh dev-runner 'cd ~/dev/www/songbooks/app/deploy && docker compose down'
```

This does **not** remove the `songbooks_db_data` named volume (the real
song library's Postgres data) — `docker compose down -v` would, so never
run that here. This compose project's name (`songbooks`, set at the top of
`docker-compose.yml`) scopes all of this to its own containers/volumes/
network; nothing else on the host is touched by any command in this file.

## Logging in (no real SMTP yet)

Login is magic-link only, and there is no real mail server wired up in
this environment (out of scope for this phase — see
`internal/mailer/console.go`). The console mailer logs the full email,
including the login link, to the `app` container's stdout *and* records it
in the `outbox` table:

```
docker compose logs app | grep -A3 'mailer.*to=you@example.com'
```

`GET /dev/outbox` on the live site also lists recent outbox entries, but
it's gated by `requireAdmin` — i.e. you must already be logged in to view
it, so it's useless for the very first login on a fresh instance. Use
`docker compose logs` for that; `/dev/outbox` is only useful for a repeat
login once you already have a session.

**Operational gap worth flagging explicitly**: until real SMTP is wired up
(tracked by the `--smtp-url` stub in `cmd/songbooks/main.go`), *nobody but
whoever can `ssh dev-runner` and read `docker compose logs`* can complete a
login on this deployment — including the site's own admin, if they're ever
somewhere without host access. This is fine for a single-admin personal
instance today but is a real blocker for inviting band members to actually
use their own accounts on this deployment; real SMTP should be the very
next infra follow-up once someone besides the admin needs to log in.

## `--dev-auto-login` / `SONGBOOKS_DEV_AUTO_LOGIN` — NEVER set this here

The app binary supports a dev-only flag/env var, `--dev-auto-login <username>`
/ `SONGBOOKS_DEV_AUTO_LOGIN=<username>`, that makes **every** request
automatically authenticated as that person — no cookie, no session, `/login`
skipped entirely. It exists purely for fast local iteration against
`songbooks_dev` on a dev machine.

**This must NEVER be set anywhere in this deployment** — not in
`docker-compose.yml`'s `environment:`, not in `deploy/.env`, not passed as a
`command:` arg. Doing so would disable authentication for every visitor to
`songbooks.tapoueh.org`, i.e. anyone on the public internet gets to act as
that person with no login step at all. If the app's startup logs
(`docker compose logs app`) ever show the loud `!!! DEV AUTO-LOGIN IS
ACTIVE !!!` banner on this host, treat it as a live incident: stop the
container immediately (`docker compose down`), remove whatever set the flag
or env var, and only bring it back up once confirmed absent.

This deployment's `docker-compose.yml` does not set it, and nothing in this
directory should ever be changed to do so.

## Passkeys (WebAuthn) — `--webauthn-rp-id` / `SONGBOOKS_WEBAUTHN_RP_ID`

Passkey login (added alongside password and magic-link — see
`internal/httpapi/webauthn.go`) needs a stable Relying Party ID (RPID): the
bare hostname a passkey is cryptographically bound to. **This MUST be set
explicitly for this deployment**, to exactly:

```
SONGBOOKS_WEBAUTHN_RP_ID=songbooks.tapoueh.org
```

— add that to `docker-compose.yml`'s `app.environment:` (or `deploy/.env`
plus an `${SONGBOOKS_WEBAUTHN_RP_ID}` reference) before passkeys are used on
this deployment. It is **not** set there as of this phase — adding it is a
deliberate, separate operational step, not something this phase pushes live
silently.

If left unset, the app instead derives the RPID from each request's own
`Host` header — a dev-only convenience for bouncing a local server between
`localhost:8080` and a LAN IP while developing, never appropriate for a
real deployment sitting behind a fixed hostname.

**A passkey is bound to the exact origin it was registered under, by
design — not a bug, not something to route around.** A passkey someone
registers locally against `localhost` will NOT work against
`songbooks.tapoueh.org`, and one registered against
`songbooks.tapoueh.org` will NOT work against `localhost` or any other
hostname — this is WebAuthn's origin-binding security property working
exactly as intended. Password and magic-link login are unaffected by any
of this (they're plain per-account credentials, not origin-bound) — a
passkey is purely an additional, optional third way in for whoever sets
one up on a given origin.

## Production and staging

Two deployments run on the host from this directory (`target.sh`):

| | production | staging |
|---|---|---|
| address | app.songbooks.studio | songbooks.tapoueh.org |
| checkout | `/var/www/songbooks` | `~/dev/www/songbooks` |
| compose project | songbooks-prod | songbooks |
| app port | 127.0.0.1:8103 | 127.0.0.1:8102 |
| stems worker | yes (`COMPOSE_PROFILES=stems`) | no |
| live lessons (Galène) | yes (`COMPOSE_PROFILES=stems,live`): each rollout builds it too and may restart it: not during a lesson | no |
| passkeys (RP ID) | app.songbooks.studio | songbooks.tapoueh.org |
| Docker engine | `default` (data on disk) | `ci-build` (data on a RAM disk) |

Each has its own `.env` (gitignored; `.env.example`), database and
backups (`deploy/backups/prod/`, `deploy/backups/staging/`, the 3 latest
kept).

The host has two Docker engines: `default` (`/var/run/docker.sock`, its
data under `/var/lib/docker` on disk) and `ci-build`
(`/run/docker-ci.sock`, the shell's default context, its data on a 48 GB
RAM disk at `/mnt/ci-docker`, lost at a reboot). Production runs on
`default`; staging, whose data can be lost, on `ci-build`. The scripts
pass the target's engine (`DOCKER_CONTEXT`) to every docker command;
by hand, in production's directory: `DOCKER_CONTEXT=default docker compose …`. Staging is for integration and tests: deploy a branch there by
checking it out in its clone, or main with `deploy/rollout.sh staging`.
`deploy/rollout.sh` alone deploys production.

## Caddy

The host's `/etc/caddy/Caddyfile` (host-specific, not in this repo; a
backup `Caddyfile.bak.pre-<change>-<unixtime>` before each edit):

```
app.songbooks.studio {
	log {
		output file /var/log/caddy/songbooks-prod-access.log
		format json
	}
	reverse_proxy 127.0.0.1:8103
}
songbooks.studio, songbooks.fr, app.songbooks.fr {
	redir https://app.songbooks.studio{uri} permanent
}
live.songbooks.studio, live.songbooks.fr {
	respond "Live lessons are coming soon." 503
}
songbooks.tapoueh.org {
	# staging: kept out of search engines
	log { … songbooks-access.log … }
	header X-Robots-Tag "noindex, nofollow, noarchive"
	respond /robots.txt …
	reverse_proxy 127.0.0.1:8102
}
```

`live.` is for the lessons' videoconference (Galène), not deployed yet.

## Live lessons (Galène) and the host's firewall

`galene/` builds Galène 1.2.2 with our client defaults (join muted,
`?headphones=1` for an unprocessed voice); the `galene` service runs in
the `live` profile, on the host's network, behind Caddy on
`live.songbooks.studio`. `host/` is the host's firewall, to apply with
`sudo host/apply-firewall.sh` before Galène (it opens UDP 40000 and
1194). The why and the steps: docs/live-lessons.md.
