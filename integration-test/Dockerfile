FROM golang:1.10

ADD . /go/src/github.com/gortc/gortcd

WORKDIR /go/src/github.com/gortc/gortcd/integration-test

RUN go install .

CMD ["integration-test"]

