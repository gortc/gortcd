ARG CI_GO_VERSION
FROM golang:${CI_GO_VERSION}

ADD . /go/src/github.com/gortc/gortcd/e2e/gortc-turn/turn-client
WORKDIR /go/src/github.com/gortc/gortcd/e2e/gortc-turn/turn-client
RUN go install .

CMD ["turn-client"]
