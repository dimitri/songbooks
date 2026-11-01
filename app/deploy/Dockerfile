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
# debian:bookworm-slim, not alpine: the LilyPond and Typst release
# tarballs below are glibc builds, and poppler-utils (pdfinfo, needed by
# internal/pdf's one-page-fit bisection loop) is a normal apt package here.
FROM debian:bookworm-slim

# ca-certificates: outbound HTTPS from the app itself (none today, but
#   cheap insurance) and used transitively by nothing at runtime — kept
#   for future-proofing the SMTP follow-up.
# poppler-utils: provides `pdfinfo`, which internal/pdf/pdf.go shells out
#   to for page-count checks in the one-page-fit bisection loop.
# fontconfig + fonts-noto: LilyPond and Typst both need a font stack for
#   fallback glyphs (v2/fonts supplies the house fonts via --font-path,
#   but fontconfig itself and a Unicode fallback font must exist on the
#   system for either binary to start cleanly).
# curl + xz-utils: only used to fetch/unpack the two binaries below; not
#   needed after this layer, but kept (small) rather than adding a
#   multi-stage copy-out step for two tarballs.
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        poppler-utils \
        fontconfig \
        fonts-noto \
        curl \
        xz-utils \
    && rm -rf /var/lib/apt/lists/*

# Typst 0.15.1 — same version as the Mac dev environment (`brew install
# typst` currently gives 0.15.1). Official release tarball, not apt (Debian
# bookworm doesn't package typst at all).
RUN set -eux; \
    curl -fsSL -o /tmp/typst.tar.xz \
        https://github.com/typst/typst/releases/download/v0.15.1/typst-x86_64-unknown-linux-musl.tar.xz; \
    tar -C /opt -xf /tmp/typst.tar.xz; \
    ln -s /opt/typst-x86_64-unknown-linux-musl/typst /usr/local/bin/typst; \
    rm /tmp/typst.tar.xz

# LilyPond 2.26.0 — same version as the Mac dev environment (`brew install
# lilypond` currently gives 2.26.0). Official release tarball, not apt:
# Debian bookworm's `lilypond` package is only 2.24.1-2, which is close but
# not the same minor version the dev pipeline was built/tested against.
RUN set -eux; \
    curl -fsSL -o /tmp/lilypond.tar.gz \
        https://gitlab.com/lilypond/lilypond/-/releases/v2.26.0/downloads/lilypond-2.26.0-linux-x86_64.tar.gz; \
    tar -C /opt -xf /tmp/lilypond.tar.gz; \
    ln -s /opt/lilypond-2.26.0/bin/lilypond /usr/local/bin/lilypond; \
    rm /tmp/lilypond.tar.gz

# Sanity check both binaries are actually runnable (not just unpacked) as
# part of the build — fail fast in `docker compose build`, not at request
# time in production.
RUN which typst lilypond && typst --version && lilypond --version

COPY --from=builder /out/songbooks /usr/local/bin/songbooks

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/songbooks"]
