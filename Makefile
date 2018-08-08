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
		-e "\/turn-client\/" \
		-e "n can be fmt.Stringer" \
		--enable-all \
		--enable="lll" --line-length=100 \
		--disable=gocyclo \
		--disable=gochecknoglobals \
		--disable=gochecknoinits \
		--disable=interfacer \
		--deadline=300s \
		--dupl-threshold=70 \
		-j $(PROCS) --vendor ./...
	@gocritic check-project .
	@echo "ok"
install:
	go get gortc.io/api
	go get -u github.com/go-critic/go-critic/...
	go get -u github.com/alecthomas/gometalinter
	gometalinter --install --update
test-e2e:
	@cd e2e && ./test.sh
test:
	@./go.test.sh
release:
	goreleaser release --rm-dist

