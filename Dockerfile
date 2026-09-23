FROM golang:1.27.1-alpine AS builder
WORKDIR $GOPATH/src/crumbhole

COPY ci-github-notifier .

# Fetch dependencies
RUN go mod tidy
RUN go mod download
RUN go mod verify

# Build the binary
RUN CGO_ENABLED=0 go build -o /go/bin/ci-github-notifier

# scratch has no /tmp, so check_run_id_file=/tmp/check_run_id (as used
# in the README and examples) fails after the check run has already been
# created. Ship an empty, world-writable /tmp so it works out of the box.
# The mode is set on the COPY, as a cross-stage COPY does not keep it.
RUN mkdir -p /out/tmp

FROM scratch

COPY --from=builder /go/bin/ci-github-notifier /go/bin/ci-github-notifier
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder --chmod=0777 /out/tmp /tmp

ENTRYPOINT ["/go/bin/ci-github-notifier"]
