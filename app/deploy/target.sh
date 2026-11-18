# target.sh — sourced by rollout.sh, backup-db.sh and restore-db.sh: which
# deployment on the host a command is for, from its first argument.
#
#   prod     app.songbooks.studio (songbooks.studio, songbooks.fr redirect
#            to it): /var/www/songbooks, port 8103, the stems worker.
#   staging  songbooks.tapoueh.org, for integration and tests:
#            ~/dev/www/songbooks (the host's dev and test area), port 8102,
#            no stems worker.
#
# Each is its own git clone and its own compose project (its .env sets
# COMPOSE_PROJECT_NAME and SONGBOOKS_PORT), with its own database.
# SONGBOOKS_DEPLOY_HOST, SONGBOOKS_DEPLOY_DIR and SONGBOOKS_HEALTH_URL
# still override.

TARGET="${1:-prod}"
case "$TARGET" in
  prod)
    DEFAULT_DIR="/var/www/songbooks"
    DEFAULT_PORT=8103
    ;;
  staging)
    DEFAULT_DIR="~/dev/www/songbooks"
    DEFAULT_PORT=8102
    ;;
  *)
    echo "unknown target \"$TARGET\": prod or staging" >&2
    exit 2
    ;;
esac
HOST="${SONGBOOKS_DEPLOY_HOST:-dev-runner-rsync}"
REMOTE_DIR="${SONGBOOKS_DEPLOY_DIR:-$DEFAULT_DIR}"
HEALTH_URL="${SONGBOOKS_HEALTH_URL:-http://127.0.0.1:$DEFAULT_PORT/}"
BACKUP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/backups/$TARGET"
