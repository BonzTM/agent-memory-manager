FROM golang:1.26-alpine AS builder

ARG VERSION=dev
ARG GO_BUILD_TAGS=""

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN set -eux; \
    BUILD_TAGS_ARG=""; \
    if [ -n "${GO_BUILD_TAGS}" ]; then BUILD_TAGS_ARG="-tags=${GO_BUILD_TAGS}"; fi; \
    go build ${BUILD_TAGS_ARG} \
      -ldflags="-s -w -X github.com/bonztm/agent-memory-manager/internal/buildinfo.Version=${VERSION}" \
      -o /out/amm ./cmd/amm && \
    go build ${BUILD_TAGS_ARG} \
      -ldflags="-s -w -X github.com/bonztm/agent-memory-manager/internal/buildinfo.Version=${VERSION}" \
      -o /out/amm-mcp ./cmd/amm-mcp && \
    go build ${BUILD_TAGS_ARG} \
      -ldflags="-s -w -X github.com/bonztm/agent-memory-manager/internal/buildinfo.Version=${VERSION}" \
      -o /out/amm-http ./cmd/amm-http

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /out/ /usr/local/bin/
RUN mkdir -p /data
EXPOSE 8080
ENTRYPOINT ["amm-http"]
