FROM golang:1.25-alpine3.23 AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY main.go .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o maxmux .

FROM alpine:3.23
COPY --from=build /app/maxmux /usr/local/bin/maxmux
ENTRYPOINT ["maxmux"]
