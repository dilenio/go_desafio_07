FROM golang:1.21 as builder-a
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o goapp

FROM alpine:latest
WORKDIR /app
COPY --from=builder-a /app/goapp .
EXPOSE 8080
CMD ["./goapp"]