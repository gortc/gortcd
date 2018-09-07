FROM golang:latest

ADD vendor /go/src/github.com/gortc/gortcd/vendor
ADD main.go /go/src/github.com/gortc/gortcd/
ADD internal /go/src/github.com/gortc/gortcd/internal

RUN go install github.com/gortc/gortcd
WORKDIR /

CMD ["gortcd"]
