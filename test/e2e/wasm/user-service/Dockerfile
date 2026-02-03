FROM golang:1.25-alpine AS build
WORKDIR /app
COPY go.mod .
COPY main.go .
RUN CGO_ENABLED=0 go build -o /bin/user-service .

FROM alpine:3.20
COPY --from=build /bin/user-service /usr/local/bin/user-service
CMD ["user-service"]
