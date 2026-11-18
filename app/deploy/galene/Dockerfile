# syntax=docker/dockerfile:1

# Galène, the videoconference server for live lessons (docs/live-lessons.md),
# built from its release tag, its web client changed in two ways:
#
# - everyone joins muted;
# - ?headphones=1 (our join page's switch) turns the microphone's processing
#   (echo cancellation, noise suppression, gain) off and high-quality audio
#   on: a singer with headphones. Without it, Galène's own defaults, right
#   for a student on laptop speakers.

FROM golang:1.26-bookworm AS builder
ARG GALENE_VERSION=galene-1.2.2
WORKDIR /src
RUN git clone --depth 1 --branch "$GALENE_VERSION" https://github.com/jech/galene.git .
RUN CGO_ENABLED=0 go build -ldflags='-s -w' -o /out/galene . \
    && cd galenectl && CGO_ENABLED=0 go build -ldflags='-s -w' -o /out/galenectl .
# The client: right after it reads its URL, before it applies its settings.
COPY client-hook.js /tmp/client-hook.js
RUN grep -c '^    let parms = new URLSearchParams(window.location.search);$' static/galene.js | grep -qx 1 \
    && perl -0pi -e 'BEGIN { local $/; open my $f, "<", "/tmp/client-hook.js"; $hook = <$f> } s/^(    let parms = new URLSearchParams\(window\.location\.search\);\n)/$1$hook/m' static/galene.js \
    && grep -q 'Songbooks: join muted' static/galene.js
RUN cp -r static /out/static

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=builder /out/galene /out/galenectl /usr/local/bin/
COPY --from=builder /out/static /galene/static
WORKDIR /galene
ENTRYPOINT ["/usr/local/bin/galene", "-static", "/galene/static", "-data", "/galene/data", "-groups", "/galene/groups", "-recordings", "/galene/recordings"]
