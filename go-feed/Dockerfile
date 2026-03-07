# --- Build stage ---
FROM golang:1.23-alpine AS builder

RUN apk add --no-cache ca-certificates

WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /feedsim ./cmd/feedsim
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /decoder ./cmd/decoder

# --- Runtime stage ---
FROM alpine:3.21

COPY --from=builder /feedsim /feedsim
COPY --from=builder /decoder /decoder

EXPOSE 8100

ENTRYPOINT ["/feedsim"]
