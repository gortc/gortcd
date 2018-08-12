ARG CI_GO_VERSION
FROM golang:${CI_GO_VERSION}

ADD vendor /go/src/github.com/gortc/gortcd/vendor
ADD main.go /go/src/github.com/gortc/gortcd/
ADD internal /go/src/github.com/gortc/gortcd/internal

WORKDIR /go/src/github.com/gortc/gortcd
RUN go install .
COPY e2e/gortcd.yml .

CMD ["gortcd"]
