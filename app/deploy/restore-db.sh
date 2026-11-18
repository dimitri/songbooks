#!/usr/bin/env bash
set -euo pipefail

# restore-db.sh — restarts the full docker compose stack on the deploy
# host and restores the Postgres database from a local backup (made by
# backup-db.sh). DESTRUCTIVE: drops and recreates the live database.
# Requires typing "yes" to confirm, unless --yes is passed.
#
# Usage: deploy/restore-db.sh [prod|staging] [path-to-dump] [--yes]
#   the target is prod by default (target.sh); path-to-dump defaults to
#   deploy/backups/<target>/latest.dump
# Env overrides: SONGBOOKS_DEPLOY_HOST, SONGBOOKS_DEPLOY_DIR

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TARGET_ARG=prod
case "${1:-}" in prod|staging) TARGET_ARG="$1"; shift ;; esac
source "$SCRIPT_DIR/target.sh" "$TARGET_ARG"
REMOTE_DIR="$REMOTE_DIR/app/deploy"

BACKUP_FILE="$BACKUP_DIR/latest.dump"
AUTO_YES=0
for arg in "$@"; do
  if [[ "$arg" == "--yes" ]]; then AUTO_YES=1; else BACKUP_FILE="$arg"; fi
done

if [[ ! -f "$BACKUP_FILE" ]]; then
  echo "No backup file at $BACKUP_FILE — run backup-db.sh first, or pass a path." >&2
  exit 1
fi

echo "This will DROP and RECREATE the $TARGET songbooks database on $HOST"
echo "and restore it from: $BACKUP_FILE"
if [[ "$AUTO_YES" -ne 1 ]]; then
  read -r -p "Type 'yes' to continue: " CONFIRM
  [[ "$CONFIRM" == "yes" ]] || { echo "Aborted."; exit 1; }
fi

echo "Restarting the $TARGET stack on $HOST ..."
ssh "$HOST" "cd $REMOTE_DIR && $DOCKER_ENV docker compose up -d --build"

echo "Waiting for the db service to be healthy ..."
until ssh "$HOST" "cd $REMOTE_DIR && $DOCKER_ENV docker compose exec -T db pg_isready -U songbooks -d songbooks" >/dev/null 2>&1; do
  sleep 1
done

echo "Stopping the app and the stems worker so they can't write mid-restore ..."
ssh "$HOST" "cd $REMOTE_DIR && $DOCKER_ENV docker compose stop app && ($DOCKER_ENV docker compose stop stems 2>/dev/null || true)"

echo "Dropping and recreating the database ..."
ssh "$HOST" "cd $REMOTE_DIR && $DOCKER_ENV docker compose exec -T db psql -U songbooks -d postgres -c 'DROP DATABASE IF EXISTS songbooks;' && $DOCKER_ENV docker compose exec -T db psql -U songbooks -d postgres -c 'CREATE DATABASE songbooks OWNER songbooks;'"

echo "Restoring from $BACKUP_FILE ..."
ssh "$HOST" "cd $REMOTE_DIR && $DOCKER_ENV docker compose exec -T db pg_restore -U songbooks -d songbooks --no-owner --role=songbooks" < "$BACKUP_FILE"

echo "Starting the app back up ..."
ssh "$HOST" "cd $REMOTE_DIR && $DOCKER_ENV docker compose up -d"

echo "Done. Verify: ssh $HOST 'curl -s -o /dev/null -w \"%{http_code}\\n\" $HEALTH_URL'"
