FROM golang:1.16.2-alpine as builder

ADD . /koinos-transaction-store
WORKDIR /koinos-transaction-store

RUN go get ./... && \
    go build -o koinos_transaction_store cmd/koinos-transaction-store/main.go

FROM alpine:latest
COPY --from=builder /koinos-transaction-store/koinos_transaction_store /usr/local/bin
ENTRYPOINT [ "/usr/local/bin/koinos_transaction_store" ]
