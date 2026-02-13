FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum main.go ./
RUN CGO_ENABLED=0 go build -o /bin/user-service .

FROM alpine:3.20
RUN apk --no-cache add ca-certificates
COPY --from=builder /bin/user-service /usr/local/bin/user-service
ENTRYPOINT ["user-service"]
