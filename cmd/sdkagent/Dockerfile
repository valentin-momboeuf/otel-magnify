FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/sdkagent ./cmd/sdkagent

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/sdkagent /usr/local/bin/sdkagent
ENTRYPOINT ["/usr/local/bin/sdkagent"]
