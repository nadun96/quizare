# syntax=docker/dockerfile:1
# The tutoring service (ADR-18, D-59): its own image, built from tutoring/.
#   docker build -t localhost/quiz-tutoring:latest tutoring/

FROM docker.io/library/golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/tutor ./cmd/tutor
# The shared secrets live on a volume at /var/lib/tutoring, owned by nonroot.
RUN mkdir -p /out/var/lib/tutoring && chmod 0700 /out/var/lib/tutoring

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/tutor /app/tutor
COPY --from=build --chown=65532:65532 /out/var/lib/tutoring /var/lib/tutoring
ENV TUTOR_LISTEN=0.0.0.0:8090
USER 65532:65532
EXPOSE 8090
HEALTHCHECK --interval=15s --timeout=5s --start-period=60s --retries=3 CMD ["/app/tutor", "healthcheck"]
ENTRYPOINT ["/app/tutor"]
