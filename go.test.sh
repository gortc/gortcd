#!/usr/bin/env bash
set -e && touch coverage.txt
# quick-test without -race
go test ./...

for d in $(go list ./... | grep -P -v "(vendor|e2e|provision)"); do
    go test -race -coverprofile=profile.out -covermode=atomic "$d"
    if [ -f profile.out ]; then
        cat profile.out >> coverage.txt
        rm profile.out
    fi
done
