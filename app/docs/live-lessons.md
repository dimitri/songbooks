# Live lessons: Galène behind Caddy, and the host's firewall

Review of November 2026, for the live lessons of coaching communities
(docs/coaching.md): a coach and one or a few students talking, singing
and demonstrating over video. Not band rehearsal in sync: WebRTC's
100–300 ms rules that out, and that would be a JamKazam-style product,
not this.

## What runs where

```
browser ──HTTPS/WSS 443──▶ Caddy ──HTTP 127.0.0.1:8444──▶ Galène
browser ──UDP 40000 (media, direct)──────────────────────▶ Galène
browser ──TURN UDP/TCP 1194 (when UDP 40000 is blocked)──▶ Galène
```

- `live.songbooks.studio` (and `live.songbooks.fr`, redirected): Caddy
  terminates TLS and proxies the web client and its websocket to Galène,
  which listens on loopback only.
- Media never goes through Caddy: browsers send it to Galène directly,
  UDP 40000 (one port, multiplexed), or through Galène's built-in TURN
  on 1194 when they can't.
- Galène uses the host's network (`network_mode: host`): behind Docker's
  NAT it would announce container addresses for its media.

## HTTPS is required

- Browsers only give a page the camera and the microphone
  (`getUserMedia`) in a secure context: HTTPS, or localhost. A plain
  HTTP videoconference doesn't work at all.
- WebRTC media is encrypted on its own (DTLS-SRTP), whatever carries the
  page; HTTPS protects the page, the websocket (signalling, chat, the
  join token) and is what makes the browser trust the page with the
  devices.
- Galène runs with `-insecure` only because Caddy does TLS in front of
  it; its `proxyURL` tells it the public `https://` address.

## How other videoconference servers do it

All put HTTPS on 443 for the page and signalling, and media on UDP
directly, with a TURN fallback:

- **Jitsi Meet**: nginx on 443 (page, websocket); the videobridge on UDP
  10000; a TURN server (coturn) for the networks that block UDP, ideally
  TURN over TLS on 443.
- **BigBlueButton**: nginx on 443; media on a UDP range; coturn on 443
  over TLS. On a single host, that's where **HAProxy** shows up: 443 is
  shared between nginx and coturn, routed by the TLS name (SNI).
- **LiveKit**: its generated deployment puts Caddy with the layer 4
  module in front, for the same reason: one port 443, the web host to the
  server, the TURN host to its TURN/TLS listener; media on UDP (a range,
  or one multiplexed port), TCP as a fallback.

So HAProxy (or Caddy's layer 4) only comes in for **TURN over TLS on
443**: the last resort of a corporate or school network that lets nothing
but HTTPS out. Galène's built-in TURN doesn't do TLS.

## Decisions

- **No HAProxy.** Caddy already does HTTPS for the app, gets the
  certificates on its own, passes websockets as they are, and serves
  HTTP/3 (UDP 443). Students are at home or in a studio: UDP 40000, or
  TURN on 1194, gets through. A second proxy in front of Caddy would add
  a moving part for a case we don't have.
- **Caddy is the way forward.** The Galène site is four lines, next to
  the app's. If students on locked-down networks turn up: a coturn with
  TLS on its own name (`turn.songbooks.studio`), shared on 443 by Caddy's
  layer 4 module (a custom Caddy build) rather than HAProxy, as LiveKit
  does; Galène then uses it instead of `-turn auto`
  (`data/ice-servers.json`).
- **Headphones optional.** By default Galène's client keeps the
  browser's echo cancellation, noise suppression and gain control: right
  for a student on laptop speakers. A join link with `?headphones=1`
  turns them off and raises the audio quality, for a singing voice
  heard as it is (`deploy/galene/client-hook.js`).
- **Everybody joins muted**, and unmutes when ready.
- **No recording** for now (`allow-recording: false`, no `record`
  permission in the tokens).

## The firewall

The host had none: everything listening on all addresses was reachable
from the internet (dev servers, Performance Co-Pilot, LLMNR, mDNS…).
`deploy/host/` brings one, in its own nftables table so it never touches
Docker's:

- `firewall.nft`: incoming connections dropped, except ssh, Caddy
  (TCP 80 and 443, UDP 443), Galène (UDP 40000, TCP and UDP 1194), ICMP,
  DHCP replies, and the containers' bridges talking to the host. A
  marked block keeps today's dev servers open: delete the lines that
  needn't be public (they stay reachable from the host, and over ssh
  tunnels).
- Containers' published ports don't go through the input chain (Docker
  forwards them): a port published on `0.0.0.0` stays public. Ours are
  on 127.0.0.1. A Postgres of another stack is published on
  `0.0.0.0:55432`: a commented rule in `firewall.nft` closes it from
  outside; better, publish it on 127.0.0.1 in its compose file.
- `apply-firewall.sh`: checks the rules, loads them, and waits for
  `keep` within two minutes, removing them otherwise (a lost ssh session
  can't lock you out). Kept, it installs `host-firewall.service`, which
  loads them at every boot. `--remove` undoes it all.

Debian's own `nftables.service` stays disabled: its
`/etc/nftables.conf` starts with `flush ruleset`, which would wipe
Docker's rules.

```
ssh dev-runner
cd /var/www/songbooks/app/deploy/host
less firewall.nft
sudo ./apply-firewall.sh
```

## Bringing Galène up

After the firewall (UDP 40000 and 1194 must be open):

```
cd /var/www/songbooks/app/deploy
galene/setup.sh                       # key in .env, config files
DOCKER_CONTEXT=ci-build docker compose --profile live build galene
DOCKER_CONTEXT=ci-build docker save songbooks-prod-galene | DOCKER_CONTEXT=default docker load
DOCKER_CONTEXT=default COMPOSE_PROFILES=stems,live docker compose up -d --no-build galene
```

and in the Caddyfile, instead of the 503:

```
live.songbooks.studio {
	reverse_proxy 127.0.0.1:8444
}
live.songbooks.fr {
	redir https://live.songbooks.studio{uri} permanent
}
```

The app side is in docs/coaching.md ("Join links"): the app reads
`SONGBOOKS_GALENE_KEY` (and `SONGBOOKS_GALENE_URL`, production's by
default) from `deploy/.env`, and signs the join tokens with it.

To try it on a laptop: build the image (`docker build -t
songbooks-galene-local deploy/galene`), give it a `data/config.json`
with `"proxyURL": "http://localhost:8444/"` and no `canonicalHost`, and
a `groups/lessons.json` from the example with a key of your own; run it
with `-insecure -http 0.0.0.0:8444 -udp-range 40000 -turn ""`, ports
8444 and 40000/udp published on 127.0.0.1; start the local app with the
same key and `SONGBOOKS_GALENE_URL=http://localhost:8444`.
