#!/usr/bin/env bash
set -euo pipefail

# rollout.sh — deploys the current state of the `main` branch (already
# pushed to GitHub) to the deploy host: backs up the database, fast-
# forwards the host's checkout, rebuilds the image, restarts the stack,
# and health-checks the result.
#
# Usage: deploy/rollout.sh [prod|staging]   (prod by default; target.sh)
# Env overrides: SONGBOOKS_DEPLOY_HOST, SONGBOOKS_DEPLOY_DIR,
#                SONGBOOKS_HEALTH_URL
#
# Prerequisite (one-time): the target's directory on the host must be a git
# clone of this repo (remote `origin` = git@github.com:dimitri/songbooks.git,
# branch `main`), not an rsync'd tree — see README.md. The host needs its
# own working GitHub access (it does, via ~/.ssh/id_ed25519_github).

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/target.sh"
echo "==> Deploying to $TARGET ($HOST:$REMOTE_DIR)"

echo "==> Backing up the database before rollout ..."
"$SCRIPT_DIR/backup-db.sh" "$TARGET"

echo "==> Fetching on $HOST ..."
ssh "$HOST" "cd $REMOTE_DIR && git fetch origin main"

LOCAL_SHA="$(git -C "$SCRIPT_DIR/../.." rev-parse origin/main)"
REMOTE_SHA="$(ssh "$HOST" "cd $REMOTE_DIR && git rev-parse origin/main")"
if [[ "$LOCAL_SHA" != "$REMOTE_SHA" ]]; then
  echo "Local origin/main ($LOCAL_SHA) differs from what $HOST just fetched" >&2
  echo "($REMOTE_SHA) — push your changes first (git push origin main)." >&2
  exit 1
fi

echo "==> Changes being deployed:"
ssh "$HOST" "cd $REMOTE_DIR && git log --oneline HEAD..origin/main" || true

echo "==> Fast-forwarding $HOST's checkout to origin/main ..."
ssh "$HOST" "cd $REMOTE_DIR && git merge --ff-only origin/main"

echo "==> Rebuilding and restarting the stack ..."
ssh "$HOST" "cd $REMOTE_DIR/app/deploy && docker compose build && docker compose up -d"

echo "==> Waiting for the app to come back up ..."
for i in $(seq 1 30); do
  CODE="$(ssh "$HOST" "curl -s -o /dev/null -w '%{http_code}' '$HEALTH_URL'" || echo 000)"
  if [[ "$CODE" == "200" ]]; then
    echo "Healthy (HTTP $CODE) after ${i}s."
    exit 0
  fi
  sleep 1
done

echo "App did not return HTTP 200 at $HEALTH_URL within 30s (last: HTTP $CODE)." >&2
echo "Check: ssh $HOST 'cd $REMOTE_DIR/app/deploy && docker compose logs app --tail=50'" >&2
exit 1
