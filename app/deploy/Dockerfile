# syntax=docker/dockerfile:1

# --- builder -----------------------------------------------------------
# Matches go.mod's `go 1.26.5` (golang:1.26-bookworm tracks the latest
# 1.26.x patch, currently 1.26.10 — fine, Go patch releases are additive).
FROM golang:1.26-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/songbooks ./cmd/songbooks
# One-off admin script, not served — queues the YouTube audio of every
# song whose reference is a YouTube video and that has none yet (a dry
# run, counts only, unless --apply; the app's workers fetch it). Built
# alongside the main binary so it ships in the same image with no extra
# build step; run via `docker compose exec app backfillaudio [--apply]`.
RUN CGO_ENABLED=0 go build -o /out/backfillaudio ./cmd/backfillaudio
# One-off admin script, same shape as backfillaudio — batch-imports
# Bands Camp's song catalog from the hand-curated legacy .song files
# in v2/BandsCamp/; see cmd/bandscampredo's own doc comment for why
# (far better source than a fresh UG/iReal fetch) and the
# docker compose run invocation (needs v2/BandsCamp mounted in, not
# baked into this image — it's reference data, not app code).
RUN CGO_ENABLED=0 go build -o /out/bandscampredo ./cmd/bandscampredo
# One-off admin script: moves songs from Riff JSON score lines to
# "gp[Track]: bars" lines and their Guitar Pro files, from a plan made by
# tools/gpmigrate/plan.cjs (see cmd/gpmigrate's own doc comment).
RUN CGO_ENABLED=0 go build -o /out/gpmigrate ./cmd/gpmigrate
# sbfiles: adds a file to a songbook's download area (cmd/sbfiles).
RUN CGO_ENABLED=0 go build -o /out/sbfiles ./cmd/sbfiles
# sbcoach: sets up a coaching community (cmd/sbcoach, docs/coaching.md).
RUN CGO_ENABLED=0 go build -o /out/sbcoach ./cmd/sbcoach
# sbdemo: turns a copy of production's data into demo data, on a local
# or staging database only (cmd/sbdemo, docs/demo-data.md).
RUN CGO_ENABLED=0 go build -o /out/sbdemo ./cmd/sbdemo
# ownership: maps the songs onto the ownership model (cmd/ownership,
# docs/ownership-model.md §9); a dry run unless --apply.
RUN CGO_ENABLED=0 go build -o /out/ownership ./cmd/ownership

# foldback: folds arrangements identical to their catalogue original back
# into it (docs/catalogue-edit-review.md); a dry run unless --apply.
RUN CGO_ENABLED=0 go build -o /out/foldback ./cmd/foldback

# albums: a band's albums, tracks and live versions from a mapping
# (docs/albums-and-live-versions.md); a dry run unless --apply.
RUN CGO_ENABLED=0 go build -o /out/albums ./cmd/albums && cp cmd/albums/otherlies.json /out/otherlies.json

# --- runtime -------------------------------------------------------------
# debian:bookworm-slim, not alpine: the LilyPond release tarball below is a
# glibc build. PDF export itself no longer needs ANY of this layer — see
# internal/pdfnative — it's pure Go, compiled into the binary above; this
# stage exists only for the (currently unused-by-default) LilyPond export
# path. Typst is gone entirely as of the native PDF renderer: no more
# `typst` binary, no more poppler-utils (pdfinfo was only ever for Typst's
# page-count bisection loop), no more /v2 bind mount requirement — PDF
# export is in-process now, which is the whole point of pdfnative.
FROM debian:bookworm-slim

# ca-certificates: outbound HTTPS from the app itself — was "none today,
#   cheap insurance" before yt-dlp (below) started making real outbound
#   requests to youtube.com, so no longer just insurance.
# fontconfig + fonts-noto: LilyPond needs a font stack for fallback glyphs.
# curl + xz-utils: only used to fetch/unpack the tarball below; not needed
#   after this layer, but kept (small) rather than a multi-stage copy-out.
# python3 + ffmpeg: yt-dlp's own runtime (it's a Python script) and the
#   encoder it shells out to for `-x --audio-format mp3` extraction — see
#   app/docs/mp3-reference-audio.md §1 for why this deployment runs its
#   own downloader rather than depending on a third-party service.
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        fontconfig \
        fonts-noto \
        curl \
        xz-utils \
        python3 \
        ffmpeg \
        unzip \
    && rm -rf /var/lib/apt/lists/*

# yt-dlp: a single self-contained Python zipapp, pinned by release tag
# (not `latest`, which YouTube's own frequent markup/API changes can
# break without warning) — download it like LilyPond above rather than
# via pip, so there's no Python package index dependency at build time.
# 2026.10.26, not the original 2025.09.26 pin: YouTube rolled out "SABR
# streaming" server-side partway through that year-plus gap, which
# breaks extraction on older yt-dlp builds entirely ("YouTube is forcing
# SABR streaming for this client" — direct report, a real failed
# download, not a hypothetical) — yt-dlp's own fix for it landed in
# later releases, so this is a real functional requirement, not just
# routine freshening.
RUN set -eux; \
    curl -fsSL -o /usr/local/bin/yt-dlp \
        https://github.com/yt-dlp/yt-dlp/releases/download/2026.10.26/yt-dlp; \
    chmod a+rx /usr/local/bin/yt-dlp
RUN which yt-dlp && yt-dlp --version

# deno: yt-dlp's own JS runtime for evaluating YouTube's signature-
# decryption code — without one it falls back to a reduced format set
# (confirmed live: "No supported JavaScript runtime could be found ...
# some formats may be missing"); extraction still worked in that one
# test, but a video whose only remaining formats need JS evaluation
# would fail outright. Official install script, same "curl a vendor's
# own installer, land the binary on PATH" pattern as yt-dlp/LilyPond
# above — it installs to $HOME/.deno by default (root's, in this image).
RUN set -eux; \
    curl -fsSL https://deno.land/install.sh | DENO_INSTALL=/usr/local sh -s -- -y; \
    which deno && deno --version

# LilyPond 2.26.0 — same version as the Mac dev environment (`brew install
# lilypond` currently gives 2.26.0). Official release tarball, not apt:
# Debian bookworm's `lilypond` package is only 2.24.1-2, which is close but
# not the same minor version the dev pipeline was built/tested against.
# Kept even though its UI button was dropped (a separate, earlier phase) —
# the Go package and route are still there; only remove this once that
# code is actually deleted, not just unlinked.
RUN set -eux; \
    curl -fsSL -o /tmp/lilypond.tar.gz \
        https://gitlab.com/lilypond/lilypond/-/releases/v2.26.0/downloads/lilypond-2.26.0-linux-x86_64.tar.gz; \
    tar -C /opt -xf /tmp/lilypond.tar.gz; \
    ln -s /opt/lilypond-2.26.0/bin/lilypond /usr/local/bin/lilypond; \
    rm /tmp/lilypond.tar.gz

# Sanity check the binary is actually runnable (not just unpacked) as part
# of the build — fail fast in `docker compose build`, not at request time.
RUN which lilypond && lilypond --version

# GeneralUser GS (docs/soundfont.md): the General MIDI bank Guitar Pro
# scores play with, served cut down to each score's presets
# (internal/soundfont). 30 MB, so fetched here rather than kept in git:
# its author's own GitHub repository at a pinned commit (it has no
# release tags; this commit is v2.0.3), checked against the SHA-256s
# below, its license next to it. deploy/fetch-soundfont.sh reads these
# ARG lines to fetch the same files for a run outside Docker.
ARG GUGS_VERSION=2.0.3
ARG GUGS_COMMIT=684543d5e5efaef08d02be50dcda8d552478fa60
ARG GUGS_SF2_SHA256=9575028c7a1f589f5770fccc8cff2734566af40cd26ed836944e9a5152688cfe
ARG GUGS_LICENSE_SHA256=7b32efefdf95ce38a043799f0659853ddc00fbaa14d8c50f0aca16b9b8b405be
RUN set -eux; \
    d=/usr/local/share/songbooks; mkdir -p "$d"; \
    base="https://raw.githubusercontent.com/mrbumpy409/GeneralUser-GS/${GUGS_COMMIT}"; \
    curl -fsSL -o "$d/GeneralUser-GS.sf2" "$base/GeneralUser-GS.sf2"; \
    curl -fsSL -o "$d/GeneralUser-GS-LICENSE.txt" "$base/documentation/LICENSE.txt"; \
    echo "${GUGS_SF2_SHA256}  $d/GeneralUser-GS.sf2" | sha256sum -c -; \
    echo "${GUGS_LICENSE_SHA256}  $d/GeneralUser-GS-LICENSE.txt" | sha256sum -c -

COPY --from=builder /out/songbooks /usr/local/bin/songbooks
COPY --from=builder /out/backfillaudio /usr/local/bin/backfillaudio
COPY --from=builder /out/bandscampredo /usr/local/bin/bandscampredo
COPY --from=builder /out/gpmigrate /usr/local/bin/gpmigrate
COPY --from=builder /out/sbfiles /usr/local/bin/sbfiles
COPY --from=builder /out/sbcoach /usr/local/bin/sbcoach
COPY --from=builder /out/sbdemo /usr/local/bin/sbdemo
COPY --from=builder /out/ownership /usr/local/bin/ownership
COPY --from=builder /out/foldback /usr/local/bin/foldback
COPY --from=builder /out/albums /usr/local/bin/albums
COPY --from=builder /out/otherlies.json /usr/local/share/songbooks/otherlies.json

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/songbooks"]
