#!/usr/bin/env bash
set -euo pipefail

# local-restore.sh — (re)creates the LOCAL docker compose stack's database
# from a production backup made by backup-db.sh, then (re)builds and
# starts the app. Destroys only the local stack's own database volume
# contents, never anything on the deploy host.
#
# Usage: deploy/local-restore.sh [path-to-dump]
#   path-to-dump defaults to deploy/backups/latest.dump (backups/ is
#   gitignored: in a worktree, pass the main checkout's dump path).

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DUMP="${1:-$SCRIPT_DIR/backups/latest.dump}"
[[ -f "$DUMP" ]] || { echo "No dump at $DUMP — run backup-db.sh first, or pass a path." >&2; exit 1; }

compose() { docker compose -f "$SCRIPT_DIR/docker-compose.local.yml" "$@"; }

compose up -d db
echo "Waiting for Postgres ..."
until compose exec -T db pg_isready -U songbooks -d songbooks >/dev/null 2>&1; do sleep 1; done

compose stop app >/dev/null 2>&1 || true
compose exec -T db psql -q -U songbooks -d postgres -c 'DROP DATABASE IF EXISTS songbooks WITH (FORCE);' -c 'CREATE DATABASE songbooks OWNER songbooks;'

echo "Restoring $DUMP ..."
compose exec -T db pg_restore -U songbooks -d songbooks --no-owner --role=songbooks < "$DUMP"

compose up -d --build app
echo "Up: http://localhost:${SONGBOOKS_LOCAL_PORT:-8102}/"
