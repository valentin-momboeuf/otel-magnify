FROM node:20-alpine AS frontend-build
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:alpine AS backend-build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY pkg/ ./pkg/
COPY --from=frontend-build /app/frontend/dist ./pkg/frontend/dist
RUN CGO_ENABLED=0 go build -o /otel-magnify ./cmd/server/

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=backend-build /otel-magnify /usr/local/bin/otel-magnify
EXPOSE 8080 4320
ENTRYPOINT ["otel-magnify"]
