FROM golang:1.24-alpine AS builder

RUN apk add --no-cache gcc musl-dev sqlite-dev

ARG VERSION=dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=1 go build -tags fts5 \
    -ldflags="-s -w -X github.com/bonztm/agent-memory-manager/internal/buildinfo.Version=${VERSION}" \
    -o /out/amm ./cmd/amm && \
    CGO_ENABLED=1 go build -tags fts5 \
    -ldflags="-s -w -X github.com/bonztm/agent-memory-manager/internal/buildinfo.Version=${VERSION}" \
    -o /out/amm-mcp ./cmd/amm-mcp && \
    CGO_ENABLED=1 go build -tags fts5 \
    -ldflags="-s -w -X github.com/bonztm/agent-memory-manager/internal/buildinfo.Version=${VERSION}" \
    -o /out/amm-http ./cmd/amm-http

FROM alpine:3.20
RUN apk add --no-cache sqlite-libs ca-certificates
COPY --from=builder /out/ /usr/local/bin/
RUN mkdir -p /data
EXPOSE 8080
ENTRYPOINT ["amm-http"]
