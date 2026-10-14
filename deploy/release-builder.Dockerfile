FROM golang:1.27.1-bookworm@sha256:8d48e12ec56735e9358640898b9d9b9fcca110612ed8a5567438c0a1baa24e66
RUN curl -fsSL https://github.com/goreleaser/goreleaser/releases/download/v2.18.3/goreleaser_Linux_x86_64.tar.gz -o /tmp/goreleaser.tar.gz \
    && echo 'b8dee4728bca1aba0d71a832a17cd0a3799f75372878e7ffa36aef2343bce0bc  /tmp/goreleaser.tar.gz' | sha256sum -c - \
    && tar -xzf /tmp/goreleaser.tar.gz -C /usr/local/bin goreleaser \
    && rm /tmp/goreleaser.tar.gz
ENTRYPOINT ["goreleaser"]
