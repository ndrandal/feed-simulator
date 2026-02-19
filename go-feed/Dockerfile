# --- Build stage ---
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /feedsim ./cmd/feedsim
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /decoder ./cmd/decoder

# --- Runtime stage ---
FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /feedsim /feedsim
COPY --from=builder /decoder /decoder

EXPOSE 8100

ENTRYPOINT ["/feedsim"]
