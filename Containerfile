# syntax=docker/dockerfile:1

# Cross-compiles on the build host for TARGETOS/TARGETARCH, so a
# multi-arch build runs the Go toolchain once instead of under emulation.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG TARGETOS TARGETARCH VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/ascender-mcp ./cmd/ascender-mcp

# distroless/static ships CA certificates, tzdata, and a nonroot user,
# which is everything a static Go binary needs at runtime.
FROM gcr.io/distroless/static-debian12:nonroot
ARG VERSION=dev
LABEL org.opencontainers.image.source="https://github.com/ctrliq/ascender-mcp" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.description="MCP server for Ascender"
COPY --from=build /out/ascender-mcp /usr/local/bin/ascender-mcp
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/ascender-mcp"]
