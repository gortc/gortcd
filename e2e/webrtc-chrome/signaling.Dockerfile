ARG CI_GO_VERSION
FROM golang:${CI_GO_VERSION}
ADD vendor /go/src/github.com/gortc/gortcd/e2e/vendor
WORKDIR /go/src/github.com/gortc/gortcd/e2e/
ADD signaling/main.go signaling/main.go
WORKDIR /go/src/github.com/gortc/gortcd/e2e/signaling
RUN go install .
CMD ["signaling"]
