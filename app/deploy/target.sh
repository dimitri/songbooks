# target.sh — sourced by rollout.sh, backup-db.sh and restore-db.sh: which
# deployment on the host a command is for, from its first argument.
#
#   prod     app.songbooks.studio (songbooks.studio, songbooks.fr redirect
#            to it): /var/www/songbooks, port 8103, the stems worker, on
#            the host's default Docker engine (its data on disk).
#   staging  songbooks.tapoueh.org, for integration and tests:
#            ~/dev/www/songbooks (the host's dev and test area), port 8102,
#            no stems worker, on the CI Docker engine (ci-build, the host's
#            default context: its data on a RAM disk, gone at a reboot).
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
    DEFAULT_CONTEXT=default
    # The default engine can't build: its bridge (docker0) is the CI
    # engine's, so a build has no network. Images are built on ci-build
    # and copied over (rollout.sh).
    BUILD_CONTEXT=ci-build
    ;;
  staging)
    DEFAULT_DIR="~/dev/www/songbooks"
    DEFAULT_PORT=8102
    DEFAULT_CONTEXT=ci-build
    BUILD_CONTEXT=ci-build
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
# Every docker command run on the host for the target goes to its engine.
RUN_CONTEXT="${SONGBOOKS_DOCKER_CONTEXT:-$DEFAULT_CONTEXT}"
DOCKER_ENV="DOCKER_CONTEXT=$RUN_CONTEXT"
BUILD_ENV="DOCKER_CONTEXT=$BUILD_CONTEXT"
