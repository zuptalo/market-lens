# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM node:22-bookworm-slim AS web
WORKDIR /web
COPY package.json package-lock.json ./
RUN npm ci
COPY index.html tsconfig.json vite.config.ts ./
COPY public ./public
COPY src ./src
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS server
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/market-lens ./cmd/market-lens

FROM alpine:3.24
LABEL org.opencontainers.image.title="Market Lens" \
      org.opencontainers.image.description="Self-hosted stock research and strategy experimentation platform." \
      org.opencontainers.image.source="https://github.com/zuptalo/market-lens" \
      org.opencontainers.image.url="https://github.com/zuptalo/market-lens"
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S marketlens \
    && adduser -S -G marketlens -h /app marketlens
WORKDIR /app
COPY --from=server /out/market-lens /app/market-lens
COPY --from=web /web/dist /app/web
RUN chown -R marketlens:marketlens /app
ENV ENV=production PORT=8080 STATIC_DIR=/app/web
USER marketlens
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/api/v1/health >/dev/null 2>&1 || exit 1
ENTRYPOINT ["/app/market-lens"]
