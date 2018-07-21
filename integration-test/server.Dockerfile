ARG CI_GO_VERSION
FROM golang:${CI_GO_VERSION}

RUN go version
ADD . /go/src/github.com/gortc/gortcd

WORKDIR /go/src/github.com/gortc/gortcd

RUN go install .

COPY integration-test/gortcd.yml .

CMD ["gortcd"]

