# syntax=docker/dockerfile:1.7
FROM golang:1.27.1-bookworm@sha256:8d48e12ec56735e9358640898b9d9b9fcca110612ed8a5567438c0a1baa24e66 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/porty ./cmd/porty

FROM debian:bookworm-20261005-slim@sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata libc6 \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system porty \
    && useradd --system --no-create-home --home-dir /home/porty --gid porty porty \
    && install -d -o porty -g porty -m 0700 /var/lib/porty /home/porty /home/porty/.docker \
    && printf '{}' > /home/porty/.docker/config.json \
    && chown porty:porty /home/porty/.docker/config.json \
    && chmod 0600 /home/porty/.docker/config.json
COPY --from=build /out/porty /usr/local/bin/porty
ENV PORTY_MONITORING_MODE=host
USER porty
VOLUME ["/var/lib/porty"]
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/porty"]
CMD ["--listen", "0.0.0.0:8080", "--data-dir", "/var/lib/porty", "--log-format", "json"]
