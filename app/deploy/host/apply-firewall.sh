#!/usr/bin/env bash
set -euo pipefail

# apply-firewall.sh — installs and starts the host firewall (firewall.nft)
# on dev-runner, with a safety net: once loaded, it waits for you to type
# "keep" within 120 seconds; otherwise (no answer, a dropped ssh session)
# it removes the firewall again, so a mistake can't lock you out.
#
#   sudo ./apply-firewall.sh            # install, load, confirm
#   sudo ./apply-firewall.sh --remove   # stop and uninstall
#
# Run it from an ssh session to the host. Read firewall.nft first.

[[ $EUID -eq 0 ]] || { echo "Run it with sudo." >&2; exit 1; }
cd "$(dirname "${BASH_SOURCE[0]}")"

if [[ "${1:-}" == "--remove" ]]; then
  systemctl disable --now host-firewall.service 2>/dev/null || true
  rm -f /etc/systemd/system/host-firewall.service /etc/host-firewall.nft
  systemctl daemon-reload
  nft delete table inet host_firewall 2>/dev/null || true
  echo "Firewall removed."
  exit 0
fi

nft -c -f firewall.nft
echo "firewall.nft checks out."

# The safety net: the firewall goes again in 120 s unless confirmed.
(sleep 120; if [[ ! -e /run/host-firewall.keep ]]; then nft delete table inet host_firewall 2>/dev/null; echo "host firewall: not confirmed, removed" | systemd-cat -t host-firewall; fi) &
NET=$!
rm -f /run/host-firewall.keep
nft -f firewall.nft
echo
echo "The firewall is on. From ANOTHER terminal, check you can still ssh in"
echo "and that https://app.songbooks.studio/ answers."
read -r -t 115 -p "Type keep to keep it (anything else, or nothing for 115 s, removes it): " ANSWER || ANSWER=""
if [[ "$ANSWER" != "keep" ]]; then
  nft delete table inet host_firewall 2>/dev/null || true
  kill "$NET" 2>/dev/null || true
  echo "Removed. Nothing installed."
  exit 1
fi
touch /run/host-firewall.keep
kill "$NET" 2>/dev/null || true

install -m 0644 firewall.nft /etc/host-firewall.nft
install -m 0644 host-firewall.service /etc/systemd/system/host-firewall.service
systemctl daemon-reload
systemctl enable host-firewall.service
systemctl restart host-firewall.service
echo "Kept, and installed: it loads at every boot (host-firewall.service)."
echo "Change it: edit firewall.nft here, run this again."
