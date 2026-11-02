# Cloud Run image: Preact UI built, embedded in a static Go binary, demo data baked in.
# Cloud Run's disk is ephemeral, so every cold start begins from deploy/cloudrun/seed.
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.24-alpine AS bin
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /skinverdict ./cmd/server

FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=bin /skinverdict /app/skinverdict
COPY data/catalog.json /app/data/catalog.json
COPY deploy/cloudrun/seed /app/data-local
ENV AGENT=rules
EXPOSE 8080
ENTRYPOINT ["/app/skinverdict"]
