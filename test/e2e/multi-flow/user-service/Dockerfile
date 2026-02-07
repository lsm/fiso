FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY main.go .
RUN CGO_ENABLED=0 go build -o user-service .

FROM alpine:3.19
COPY --from=builder /app/user-service /user-service
EXPOSE 8082
CMD ["/user-service"]
