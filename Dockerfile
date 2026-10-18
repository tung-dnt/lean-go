# syntax=docker/dockerfile:1

# ---------------------------------------------------------------------------
# Stage 0: JS dependencies
# ---------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM oven/bun:1.4.2-alpine AS jsdeps
WORKDIR /src
COPY package.json bun.lock ./
RUN --mount=type=cache,target=/root/.bun/install/cache \
  bun install --frozen-lockfile --production

# ---------------------------------------------------------------------------
# Stage 1: build
# ---------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

# Toolchain for the justfile's asset recipes — same commands as local dev, so
# there is no duplicated download logic here. libstdc++/libgcc: the standalone
# Tailwind binary is Bun-compiled C++ and links against them even in the -musl
# build; Alpine omits them by default.
RUN apk add --no-cache just curl libstdc++ libgcc

# Fetch the pinned Tailwind CLI + daisyui plugins via `just tailwind-install`
# (versions from versions.env; arch/musl auto-detected in the justfile). Own
# early layer so it caches across source changes. .dockerignore keeps the host
# copies out, so the later `COPY . .` won't clobber what this downloads.
COPY justfile versions.env ./
RUN just tailwind-install

# Dependency layer: only invalidated when go.mod/go.sum change.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
  go mod download

COPY . .

# node_modules from the jsdeps stage — esbuild resolves htmx/Alpine from here.
COPY --from=jsdeps /src/node_modules ./node_modules

# Regenerate the embedded assets (public/static/main.css + the js/ bundle) with the
# same justfile recipes local dev uses, so build output never ships stale. Runs on
# every source change; the tool download above stays cached.
RUN --mount=type=cache,target=/go/pkg/mod \
  --mount=type=cache,target=/root/.cache/go-build \
  just css js

# -tags timetzdata embeds the zoneinfo database, which scratch does not carry.
RUN --mount=type=cache,target=/go/pkg/mod \
  --mount=type=cache,target=/root/.cache/go-build \
  CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
  go build -trimpath -tags timetzdata -ldflags="-s -w" -o /out/web ./cmd/web

# scratch has no shell, so anything the app expects to exist at runtime has to
# be staged here and copied in:
#   /tmp          net/http spills multipart uploads over 32 MiB to os.TempDir()
RUN mkdir -p /out/rootfs/tmp /out/rootfs/app \
  && chmod 1777 /out/rootfs/tmp \
  && echo 'app:x:10001:10001::/app:/sbin/nologin' > /out/passwd \
  && echo 'app:x:10001:' > /out/group

# ---------------------------------------------------------------------------
# Stage 2: runtime
# ---------------------------------------------------------------------------
FROM scratch

# Outbound HTTPS (any third-party API, SMTP over TLS) needs the trust store.
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /out/passwd /etc/passwd
COPY --from=builder /out/group /etc/group
COPY --from=builder --chown=10001:10001 /out/rootfs/ /
COPY --from=builder /out/web /usr/local/bin/

# viper resolves ./config/config.yaml relative to the working directory and
# fails hard if it is missing. Every value is overridable at runtime with
# GO_-prefixed env vars (GO_DATABASE_CONNECTION, GO_APP_HOST,
# GO_APP_ENCRYPTIONKEY, GO_APP_ENVIRONMENT, ...) — do not bake
# environment-specific values into the image.
COPY --chown=10001:10001 config/config.yaml /app/config/config.yaml

WORKDIR /app
USER 10001:10001

EXPOSE 8000

# No HEALTHCHECK: scratch has no shell or wget to exec. Probe from outside
# (compose `depends_on`, or an orchestrator HTTP/TCP probe against :8000).
ENTRYPOINT ["/usr/local/bin/web"]
