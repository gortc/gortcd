FROM golang:1.10

ADD . /go/src/github.com/gortc/gortcd

WORKDIR /go/src/github.com/gortc/gortcd

RUN go install .

CMD ["gortcd"]

