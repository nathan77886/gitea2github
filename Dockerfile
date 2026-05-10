# ── Build stage ────────────────────────────────────────────────────────────────
FROM golang:1.24-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /gitea2github ./cmd

# ── Runtime stage ──────────────────────────────────────────────────────────────
FROM alpine:3.21

# Install git (required for repository operations).
RUN apk add --no-cache git openssh-client

# Non-root user.
RUN addgroup -S app && adduser -S app -G app

WORKDIR /home/app

# Copy the binary.
COPY --from=builder /gitea2github /usr/local/bin/gitea2github

# Default data directories (mount a volume here in production).
RUN mkdir -p /data/work /data/queue && chown -R app:app /data

USER app

EXPOSE 8080

ENTRYPOINT ["gitea2github"]
CMD ["-config", "/home/app/config.yaml"]
