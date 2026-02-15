FROM golang:1.25-alpine AS build
WORKDIR /app
COPY go.mod .
COPY main.go .
RUN CGO_ENABLED=0 go build -o /bin/backend-service .

FROM alpine:3.20
COPY --from=build /bin/backend-service /usr/local/bin/backend-service
CMD ["backend-service"]
