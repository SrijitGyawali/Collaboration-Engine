# syntax=docker/dockerfile:1
# Builds any service in cmd/:
#   docker build --build-arg SERVICE=gateway -t collab-gateway .
FROM golang:1.26-alpine AS build
ARG SERVICE
RUN test -n "$SERVICE" || { echo "pass --build-arg SERVICE=<a folder in cmd/>" >&2; exit 1; }
WORKDIR /src

# Modules first, so this layer is reused until go.mod or go.sum change. The
# cache mounts keep downloaded modules and compiled packages between builds.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/${SERVICE}

# A static binary on distroless: no shell, no package manager, runs as a
# non-root user.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
ENTRYPOINT ["/app"]
