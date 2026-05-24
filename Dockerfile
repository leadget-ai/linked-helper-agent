FROM golang:1.24-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown

# CGO disabled — modernc.org/sqlite is pure Go, so we get a fully static
# binary that runs on alpine/scratch without libc.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /build/lh-agent ./cmd/agent

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /build/lh-agent /app/lh-agent

# Hard cap so the agent stays well below 128 MiB on a memory-tight box.
# GOGC=50 trades a bit of CPU for more aggressive collection, which matters
# because we hold SQLite handles for the lifetime of the process.
ENV GOMEMLIMIT=128MiB
ENV GOGC=50

ENTRYPOINT ["/app/lh-agent"]
