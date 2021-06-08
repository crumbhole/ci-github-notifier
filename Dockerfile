FROM golang:alpine AS builder
WORKDIR $GOPATH/src/sendible.com/sendible-labs

COPY ci-github-notifier .

# Fetch dependencies
RUN go mod download
RUN go mod verify

# Build the binary
RUN CGO_ENABLED=0 go build -o /go/bin/ci-github-notifier

# FROM scratch

# COPY --from=builder /go/bin/ci-github-notifier /go/bin/ci-github-notifier

ENTRYPOINT ["/go/bin/ci-github-notifier"]