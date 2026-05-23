# --- Build stage -------------------------------------------------------------
FROM golang:1.24-alpine AS build
WORKDIR /src

# Cache dependencies first.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/api ./cmd/api

# --- Runtime stage -----------------------------------------------------------
FROM alpine:3.20
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 appuser
WORKDIR /app

COPY --from=build /out/api /app/api
# Migrations are read at runtime by the embedded migrator.
COPY internal/infrastructure/persistence/postgres/migrations /app/internal/infrastructure/persistence/postgres/migrations

USER appuser
EXPOSE 8081
ENTRYPOINT ["/app/api"]
