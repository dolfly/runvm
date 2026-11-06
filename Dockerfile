# syntax=docker/dockerfile:1

# Build stage: pure Go, no cgo. webframe loads the system WebView and golem
# loads libunicorn at runtime through purego, so the build needs no C toolchain
# and the binary carries no bundled engine.
FROM golang:1.27-bookworm AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -tags unicorn -trimpath \
        -ldflags "-s -w -X main.version=${VERSION}" \
        -o /out/runvm ./cmd/runvm

# golem's bionic sysroot lives inside its module; resolve it by module path so
# a version bump does not silently point at a stale directory.
RUN mkdir -p /out/assets && \
    cp -r "$(go list -m -f '{{.Dir}}' github.com/isesword/golem)/assets/." /out/assets/ && \
    ls /out/assets/android/sdk23/lib64/libc.so >/dev/null

# Runtime stage: the libraries the two backends need, and nothing else.
FROM debian:bookworm-slim

ARG VERSION=dev
LABEL org.opencontainers.image.title="runvm" \
      org.opencontainers.image.description="MCP test environments: Android .so through golem's emulator, H5 pages through webframe" \
      org.opencontainers.image.source="https://github.com/dolfly/runvm" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.licenses="MIT"

ENV DEBIAN_FRONTEND=noninteractive

# WebKitGTK (the engine), Xvfb + xauth (webframe boots its own display when
# there is none), mesa (software GL under Xvfb), x11-apps for xwd — which is how
# h5_screenshot reads the virtual screen — and fonts, because a page with CJK
# text renders as boxes without them.
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        libwebkit2gtk-4.1-0 libjavascriptcoregtk-4.1-0 \
        xvfb xauth x11-apps \
        libgl1-mesa-dri libglx-mesa0 \
        fonts-dejavu-core fonts-noto-cjk \
        libunicorn2 \
    && rm -rf /var/lib/apt/lists/*

# Not every suite ships libunicorn2; golem only needs the shared object, and the
# python wheel carries a suitable one.
RUN set -eux; \
    if ! ldconfig -p | grep -q 'libunicorn\.so\.2'; then \
        echo "libunicorn2 missing from this suite; taking it from the python wheel"; \
        apt-get update; \
        apt-get install -y --no-install-recommends python3 python3-pip; \
        rm -rf /var/lib/apt/lists/*; \
        pip3 install --quiet --break-system-packages unicorn; \
        lib="$(python3 -c 'import os, unicorn; print(os.path.join(os.path.dirname(unicorn.__file__), "lib", "libunicorn.so.2"))')"; \
        ln -sf "$lib" /usr/local/lib/libunicorn.so.2; \
        ldconfig; \
    fi

COPY --from=build /out/runvm /usr/local/bin/runvm
COPY --from=build /out/assets /opt/runvm/assets

# The sysroot is data, not a build-time path: point the binary at the copy in
# the image instead of the module cache it was compiled against.
ENV RUNVM_ASSETS=/opt/runvm/assets

RUN useradd --create-home --uid 10001 runvm && mkdir -p /work && chown runvm:runvm /work
USER runvm
WORKDIR /work

EXPOSE 8765
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s \
    CMD ["/usr/local/bin/runvm", "version"]

# No arguments means the MCP server on stdio, which is what a client expects
# from `docker run -i`. For remote use: runvm http -addr 0.0.0.0:8765.
ENTRYPOINT ["/usr/local/bin/runvm"]
