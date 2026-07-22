# syntax=docker/dockerfile:1

# ---- build stage -----------------------------------------------------------
# Pinned Go version matching go.mod. Build is fully static (CGO disabled) so the
# binary runs in a distroless/scratch image with no libc.
FROM golang:1.23-alpine AS build

WORKDIR /src

# Cache module downloads separately from source so code changes don't re-fetch deps.
COPY go.mod go.sum* ./
RUN go mod download

COPY . .

# -trimpath + ldflags strip local paths and debug info: smaller, reproducible,
# nothing leaking the build machine's filesystem layout.
RUN CGO_ENABLED=0 GOOS=linux GOFLAGS=-mod=mod \
    go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

# ---- runtime stage ---------------------------------------------------------
# Distroless: no shell, no package manager, minimal attack surface. `nonroot`
# runs as uid 65532 — the container never runs as root.
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=build /out/api /app/api
# Migrations shipped alongside so they can be applied from the same image.
COPY --from=build /src/migrations /app/migrations

USER nonroot:nonroot
EXPOSE 8080

# Liveness endpoint doubles as the container HEALTHCHECK.
ENTRYPOINT ["/app/api"]
