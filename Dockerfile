# syntax=docker/dockerfile:1.7
FROM golang:1.27.1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/porty ./cmd/porty

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S porty \
    && adduser -S -D -H -G porty porty \
    && install -d -o porty -g porty -m 0700 /var/lib/porty
COPY --from=build /out/porty /usr/local/bin/porty
USER porty
VOLUME ["/var/lib/porty"]
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/porty"]
CMD ["--listen", "0.0.0.0:8080", "--data-dir", "/var/lib/porty", "--log-format", "json"]
