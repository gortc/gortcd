#!/usr/bin/env bash
set -e && touch coverage.txt

for d in $(go list ./... | grep -P -v "(vendor|e2e|provision)"); do
    go test -coverprofile=profile.out -covermode=atomic "$d"
    if [ -f profile.out ]; then
        cat profile.out >> coverage.txt
        rm profile.out
    fi
done
