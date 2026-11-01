# songbooks deployment (dev-runner)

This directory is the self-contained deployment artifact for the songbooks
app: a multi-stage `Dockerfile` (Go binary + the `typst`/`lilypond`/
`pdfinfo` CLIs the PDF export routes shell out to) and a `docker-compose.yml`
wiring it to a Postgres 16 container. It's built to run on `dev-runner`
(the shared host already running the gites/pr0/faits-politiques stacks),
reverse-proxied by the host's existing Caddy under `songbooks.tapoueh.org`.

## Layout on the host

```
~/dev/www/songbooks/
  app/            <- this repo's app/ subtree (rsync'd or git-cloned)
  v2/             <- a copy of songbooks/v2 (Typst templates + fonts)
  deploy/.env     <- real secrets, not committed anywhere
```

`docker-compose.yml`'s build context is `..` relative to this `deploy/`
directory, i.e. the `app/` directory itself — so `deploy/` must stay
checked out as a subdirectory of `app/`, same as in this repo.

## First deploy

1. Copy the app tree and the v2/ content tree to the host:

   ```
   rsync -a --delete /Users/dim/dev/songbooks/app/  dev-runner:~/dev/www/songbooks/app/
   rsync -a --delete /Users/dim/dev/songbooks/v2/   dev-runner:~/dev/www/songbooks/v2/
   ```

2. On the host, create `~/dev/www/songbooks/app/deploy/.env` from
   `.env.example` (real `POSTGRES_PASSWORD`, and `V2_HOST_PATH` pointing at
   the `v2/` copy from step 1 — e.g. `/home/dim/dev/www/songbooks/v2`).

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

```
rsync -a --delete /Users/dim/dev/songbooks/app/ dev-runner:~/dev/www/songbooks/app/
ssh dev-runner 'cd ~/dev/www/songbooks/app/deploy && docker compose build && docker compose up -d'
```

If `v2/` content changed (new songs imported locally, templates edited),
re-rsync it too:

```
rsync -a --delete /Users/dim/dev/songbooks/v2/ dev-runner:~/dev/www/songbooks/v2/
```

No restart is needed for a `v2/` content change alone — the app reads it at
request time.

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

## Caddy

The host's `/etc/caddy/Caddyfile` has a `songbooks.tapoueh.org` block
reverse-proxying to `127.0.0.1:8102` (this compose file's published app
port), added following the existing house style (see the other
`*.gites.tapoueh.org` blocks). The edit's pre-change backup lives at
`/etc/caddy/Caddyfile.bak.pre-songbooks-<unixtimestamp>` on the host (see
the phase report for the exact filename). Caddy config itself is not part
of this repo — it's host-specific infra, edited directly on `dev-runner`.
