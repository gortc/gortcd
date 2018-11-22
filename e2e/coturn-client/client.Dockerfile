ARG CI_GO_VERSION
FROM golang:${CI_GO_VERSION} as builder

ADD vendor /go/src/github.com/gortc/gortcd/vendor
ADD e2e/coturn-client/wait.go /go/src/github.com/gortc/gortcd/e2e/coturn-client/

WORKDIR /go/src/github.com/gortc/gortcd/e2e/coturn-client
RUN go build -o /wait-turn .

FROM gortc/coturn
COPY --from=builder /wait-turn /usr/bin/
ADD e2e/coturn-client/client.sh /usr/bin