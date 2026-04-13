FROM node:20-alpine AS frontend-build
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:alpine AS backend-build
WORKDIR /app/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=frontend-build /app/frontend/dist ./cmd/server/dist
RUN CGO_ENABLED=0 go build -o /otel-magnify ./cmd/server/

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=backend-build /otel-magnify /usr/local/bin/otel-magnify
EXPOSE 8080 4320
ENTRYPOINT ["otel-magnify"]
