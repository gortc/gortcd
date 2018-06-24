ARG CI_GO_VERSION
FROM golang:${CI_GO_VERSION}

ADD . /go/src/github.com/gortc/gortcd

WORKDIR /go/src/github.com/gortc/gortcd/integration-test

RUN go install .

CMD ["integration-test"]

