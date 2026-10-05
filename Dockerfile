# syntax=docker/dockerfile:1
# Multi-stage build for the QR Classroom Quiz Platform (D-37).
# Works with `docker build` and `podman build`; image names are fully qualified
# so Podman needs no unqualified-search registries.
#
# The build context is the repository root: the docs site embeds DECISIONS.md.

# ---- 1. SvelteKit static SPA ------------------------------------------------
FROM docker.io/library/node:22-alpine AS web
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY DECISIONS.md /src/DECISIONS.md
COPY frontend/ ./
RUN npm run build

# ---- 2. Go binary -------------------------------------------------------------
FROM docker.io/library/golang:1.26-alpine AS server
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server
# Distroless has no shell, so the directories are prepared here: the KEK lives
# on a volume mounted at /var/lib/quiz, owned by the nonroot user (65532).
RUN mkdir -p /out/var/lib/quiz/secrets && chmod 0700 /out/var/lib/quiz/secrets

# ---- 3. Runtime ---------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server /out/server /app/server
COPY --from=web /src/frontend/build /app/web
COPY --from=server --chown=65532:65532 /out/var/lib/quiz /var/lib/quiz
COPY LICENSE /app/LICENSE
ENV QP_LISTEN=0.0.0.0:8080 \
    QP_STATIC_DIR=/app/web \
    QP_KEK_FILE=/var/lib/quiz/secrets/kek
USER 65532:65532
EXPOSE 8080
VOLUME ["/var/lib/quiz"]
HEALTHCHECK --interval=15s --timeout=5s --start-period=60s --retries=3 CMD ["/app/server", "healthcheck"]
ENTRYPOINT ["/app/server"]
