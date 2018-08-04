ARG CI_GO_VERSION
FROM golang:${CI_GO_VERSION}

ADD . /go/src/github.com/gortc/gortcd

WORKDIR /go/src/github.com/gortc/gortcd/e2e

RUN go install .

CMD ["e2e"]

