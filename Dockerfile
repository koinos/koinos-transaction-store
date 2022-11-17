FROM golang:1.18-alpine as builder

ADD . /koinos-transaction-store
WORKDIR /koinos-transaction-store

RUN apk update && \
    apk add \
        gcc \
        musl-dev \
        linux-headers \
        git

RUN go get ./... && \
    go build -ldflags="-X main.Commit=$(git rev-parse HEAD)" -o koinos_transaction_store cmd/koinos-transaction-store/main.go

FROM alpine:latest
COPY --from=builder /koinos-transaction-store/koinos_transaction_store /usr/local/bin
ENTRYPOINT [ "/usr/local/bin/koinos_transaction_store" ]
