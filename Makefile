PROCS := $(shell nproc)
prepush: test lint test-e2e
lint:
	@echo "linting on $(PROCS) cores"
	@gometalinter -e "\.String\(\).+gocyclo" \
		-e "(_test.go|e2e).+(gocyclo|errcheck|dupl|lll|vetshadow)" \
		-e "e2e\/.+(gosec)" \
		-e "(e2e|gortcd-turn-client|gortcd-turn-client)\/.+(gosec|errcheck)" \
		-e "commands\/.+(lll)" \
		-e "isZeroOrMore is a pure function but its return value is ignored" \
		-e "isOptional is a pure function but its return value is ignored" \
		-e "parameter result 0 \(bool\) is never used" \
		-e "parameter d always receives \"IN\"" \
		-e "e2e\/.+field \S+ is unused" \
		-e "\/turn-client\/" \
		-e "n can be fmt.Stringer" \
		--enable-all \
		--enable="lll" --line-length=120 \
		--disable=gocyclo \
		--disable=gochecknoglobals \
		--disable=gochecknoinits \
		--disable=interfacer \
		--deadline=300s \
		--dupl-threshold=70 \
		-j $(PROCS) --vendor ./...
	@gocritic check-project --exclude "testutil|testdata|vendor|builtin" .
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
