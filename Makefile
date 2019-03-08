PROCS := $(shell nproc)
prepush: test lint test-e2e
lint:
	@golangci-lint run ./...
	@echo "ok"
install:
	go get gortc.io/api
	go get -u github.com/go-critic/go-critic/...
	go get -u github.com/alecthomas/gometalinter
	gometalinter --install --update
test-e2e-webrtc:
	@cd e2e/webrtc-chrome && ./test.sh
test-e2e-coturn:
	@cd e2e/coturn-client && ./test.sh
test-e2e: test-e2e-webrtc test-e2e-coturn
test:
	@./go.test.sh
test-fast:
	@./go.test-fast.sh
release:
	goreleaser release --rm-dist
