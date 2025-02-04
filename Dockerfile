FROM golang:1.23.5-alpine AS builder
WORKDIR $GOPATH/src/crumbhole

COPY ci-github-notifier .

# Fetch dependencies
RUN go mod tidy
RUN go mod download
RUN go mod verify

# Build the binary
RUN CGO_ENABLED=0 go build -o /go/bin/ci-github-notifier

FROM scratch

COPY --from=builder /go/bin/ci-github-notifier /go/bin/ci-github-notifier
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

ENTRYPOINT ["/go/bin/ci-github-notifier"]
