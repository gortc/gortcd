FROM golang:1.10

ADD . /go/src/github.com/gortc/turn

WORKDIR /go/src/github.com/gortc/turn/integration-test

RUN go install .

CMD ["integration-test"]

