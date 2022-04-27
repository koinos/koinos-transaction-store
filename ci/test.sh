#!/bin/bash

set -e
set -x

if [[ -z $BUILD_DOCKER ]]; then
   go test -v github.com/koinos/koinos-transaction-store/internal/trxstore -coverprofile=./build/transactionstore.out -coverpkg=./internal/trxstore
   gcov2lcov -infile=./build/transactionstore.out -outfile=./build/transactionstore.info

   golangci-lint run ./...
else
   TAG="$TRAVIS_BRANCH"
   if [ "$TAG" = "master" ]; then
      TAG="latest"
   fi

   export TRANSACTION_STORE_TAG=$TAG

   git clone https://github.com/koinos/koinos-integration-tests.git

   cd koinos-integration-tests
   go get ./...
   cd tests
   ./run.sh
fi
