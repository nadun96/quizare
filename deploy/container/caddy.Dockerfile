# Caddy with the layer4 plugin, for tutoring's relay on port 443 (D-83):
# it hands connections for the relay's name to LiveKit's TURN and serves
# everything else as usual. Versions as tested together.
FROM docker.io/library/caddy:2-builder AS build
RUN xcaddy build v2.11.7 --with github.com/mholt/caddy-l4@v0.1.2

FROM docker.io/library/caddy:2-alpine
COPY --from=build /usr/bin/caddy /usr/bin/caddy
