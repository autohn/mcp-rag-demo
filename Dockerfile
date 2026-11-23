# Frontend
FROM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Backend
FROM golang:1.27-alpine AS api
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /server ./cmd/server

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
COPY --from=api /server /app/server
COPY --from=web /web/dist /app/web
ENV WEB_DIR=/app/web DATA_DIR=/data HTTP_ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["/app/server"]
