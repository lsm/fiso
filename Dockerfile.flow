FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/fiso-flow ./cmd/fiso-flow

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /bin/fiso-flow /usr/local/bin/fiso-flow

RUN adduser -D -u 1000 fiso
USER fiso

EXPOSE 9090

ENTRYPOINT ["fiso-flow"]
