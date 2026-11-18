#!/usr/bin/env bash
set -euo pipefail

# backup-db.sh — dumps the deploy host's Postgres database (docker
# compose's "db" service) over SSH and saves it locally, timestamped.
# Local backups are never committed — see deploy/.gitignore.
#
# Usage: deploy/backup-db.sh
# Env overrides: SONGBOOKS_DEPLOY_HOST (default dev-runner-rsync),
#                SONGBOOKS_DEPLOY_DIR  (default ~/dev/www/songbooks/app/deploy),
#                SONGBOOKS_DUMP_COMPRESS (default zstd:9),
#                SONGBOOKS_BACKUPS_KEEP (default 3: older dumps are removed)

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
#
# Compressed with zstd (pg_dump 16): most of the database is audio
# (reference recordings and their parts, MP3, already compressed), so
# no level gains much on it; measured on production (1 GB): gzip, the
# default, 1064 MB; zstd:9, 966 MB, in the same time. COMPRESS overrides.
COMPRESS="${SONGBOOKS_DUMP_COMPRESS:-zstd:9}"
ssh "$HOST" "cd $REMOTE_DIR && docker compose exec -T db pg_dump -U songbooks -d songbooks --format=custom --compress=$COMPRESS" > "$OUT_FILE.part"
mv "$OUT_FILE.part" "$OUT_FILE"

ln -sf "$(basename "$OUT_FILE")" "$OUT_DIR/latest.dump"
echo "Backup complete: $OUT_FILE ($(du -h "$OUT_FILE" | cut -f1))"
echo "Symlinked as $OUT_DIR/latest.dump"

# Only the most recent few are kept (KEEP, 3 by default): every deploy
# makes one, each about a gigabyte.
KEEP="${SONGBOOKS_BACKUPS_KEEP:-3}"
ls -1t "$OUT_DIR"/songbooks-*.dump 2>/dev/null | tail -n +"$((KEEP + 1))" | while read -r old; do
  echo "Removing old backup $(basename "$old")"
  rm -f -- "$old"
done
rm -f "$OUT_DIR"/songbooks-*.dump.part
