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
    && rm -rf /var/lib/apt/lists/*

# yt-dlp: a single self-contained Python zipapp, pinned by release tag
# (not `latest`, which YouTube's own frequent markup/API changes can
# break without warning) — download it like LilyPond above rather than
# via pip, so there's no Python package index dependency at build time.
RUN set -eux; \
    curl -fsSL -o /usr/local/bin/yt-dlp \
        https://github.com/yt-dlp/yt-dlp/releases/download/2025.09.26/yt-dlp; \
    chmod a+rx /usr/local/bin/yt-dlp
RUN which yt-dlp && yt-dlp --version

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

COPY --from=builder /out/songbooks /usr/local/bin/songbooks

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/songbooks"]
