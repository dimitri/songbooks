#!/usr/bin/env bash
set -euo pipefail

# setup.sh — run once on the host, in production's app/deploy/galene/:
# writes Galène's configuration from the examples, with a new signing key
# shared with the app (deploy/.env: SONGBOOKS_GALENE_KEY), the key that
# signs the lessons' join tokens (docs/live-lessons.md). Keeps an existing
# key: run it again to refresh the files from the examples.

cd "$(dirname "${BASH_SOURCE[0]}")"
ENV=../.env
mkdir -p data groups
KEY="$(grep -s '^SONGBOOKS_GALENE_KEY=' "$ENV" | cut -d= -f2- || true)"
if [[ -z "$KEY" ]]; then
  # 32 random bytes, base64url without padding (a JWK "k").
  KEY="$(openssl rand -base64 32 | tr '+/' '-_' | tr -d '=\n')"
  echo "SONGBOOKS_GALENE_KEY=$KEY" >> "$ENV"
  echo "A new signing key is in $ENV."
fi
umask 077
cp config.json.example data/config.json
sed "s|GALENE_KEY|$KEY|" lessons.json.example > groups/lessons.json
echo "Galène's configuration: data/config.json, groups/lessons.json."
