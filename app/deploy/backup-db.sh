#!/usr/bin/env bash
set -euo pipefail

# backup-db.sh — dumps the deploy host's Postgres database (docker
# compose's "db" service) over SSH and saves it locally, timestamped.
# Local backups are never committed — see deploy/.gitignore.
#
# Usage: deploy/backup-db.sh
# Env overrides: SONGBOOKS_DEPLOY_HOST (default dev-runner-rsync),
#                SONGBOOKS_DEPLOY_DIR  (default ~/dev/www/songbooks/app/deploy)

HOST="${SONGBOOKS_DEPLOY_HOST:-dev-runner-rsync}"
REMOTE_DIR="${SONGBOOKS_DEPLOY_DIR:-~/dev/www/songbooks/app/deploy}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT_DIR="$SCRIPT_DIR/backups"
mkdir -p "$OUT_DIR"

TS="$(date -u +%Y%m%dT%H%M%SZ)"
OUT_FILE="$OUT_DIR/songbooks-$TS.dump"

echo "Backing up songbooks DB from $HOST ..."
# Custom format (-Fc), not plain SQL: lets restore-db.sh use pg_restore,
# which can target a specific role/schema and skip ownership statements
# (--no-owner) — the role name inside the dump might not exist verbatim
# on a future restore target, same reasoning a plain `pg_dump | psql`
# pipeline would need extra flags for anyway.
#
# -U/-d hardcoded to "songbooks": that's docker-compose.yml's own
# default (POSTGRES_USER/POSTGRES_DB), confirmed against the actual
# running deploy — not read from deploy/.env here since docker compose
# already resolves that file itself when this ssh command runs `exec`
# from the compose project directory.
ssh "$HOST" "cd $REMOTE_DIR && docker compose exec -T db pg_dump -U songbooks -d songbooks --format=custom" > "$OUT_FILE"

ln -sf "$(basename "$OUT_FILE")" "$OUT_DIR/latest.dump"
echo "Backup complete: $OUT_FILE ($(du -h "$OUT_FILE" | cut -f1))"
echo "Symlinked as $OUT_DIR/latest.dump"
