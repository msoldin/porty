# Disposable portability test image: no file capabilities or privileged tools.
FROM alpine:3.22
COPY --chmod=0555 traffic.test /traffic.test
USER 1000:1000
ENTRYPOINT ["/traffic.test"]
