FROM golang:bookworm AS builder

WORKDIR /build
COPY . .
RUN go build -o ./bin/ernesto ./cmd/...

FROM debian:bookworm

COPY --from=builder /build/bin/ernesto /usr/bin/ernesto
ENTRYPOINT /usr/bin/ernesto
