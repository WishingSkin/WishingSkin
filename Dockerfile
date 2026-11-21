FROM node:24.19.0-alpine AS frontend
WORKDIR /src/web
RUN npm install -g pnpm@11.24.0
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
COPY internal/httpapi/admin_event_labels.json /src/internal/httpapi/admin_event_labels.json
COPY internal/httpapi/page_routes.json /src/internal/httpapi/page_routes.json
RUN pnpm build

FROM golang:1.26.11-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -o /wishingskin ./cmd/wishingskin

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && mkdir -p /data/textures /data/backups && chown -R 65532:65532 /data
COPY --from=backend /wishingskin /usr/local/bin/wishingskin
COPY LICENSE /usr/share/licenses/wishingskin/LICENSE
COPY THIRD_PARTY_NOTICES.md /usr/share/licenses/wishingskin/THIRD_PARTY_NOTICES.md
USER 65532:65532
WORKDIR /data
ENV WS_LISTEN=0.0.0.0:8090 WS_TEXTURE_DIR=/data/textures
EXPOSE 8090
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 CMD ["wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8090/readyz"]
ENTRYPOINT ["wishingskin"]
CMD ["serve"]
