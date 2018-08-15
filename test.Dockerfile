FROM golang:latest

RUN go get -u github.com/alecthomas/gometalinter
RUN gometalinter --install --update
RUN go get -u github.com/go-critic/go-critic/...

ADD go.test.sh /go/src/github.com/gortc/gortcd/
ADD vendor /go/src/github.com/gortc/gortcd/vendor
ADD main.go /go/src/github.com/gortc/gortcd/
ADD internal /go/src/github.com/gortc/gortcd/internal
ADD Makefile /go/src/github.com/gortc/gortcd/

WORKDIR /go/src/github.com/gortc/gortcd/
RUN make test
RUN make lint
