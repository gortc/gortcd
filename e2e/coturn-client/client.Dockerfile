ARG CI_GO_VERSION
FROM golang:${CI_GO_VERSION} as builder

ADD e2e/coturn-client/go.mod /src/coturn-client/
ADD e2e/coturn-client/go.sum /src/coturn-client/
WORKDIR /src/coturn-client/
RUN go mod download

ADD e2e/coturn-client/wait.go /src/coturn-client/

WORKDIR /src/coturn-client
RUN go build -o /wait-turn .

FROM gortc/coturn
COPY --from=builder /wait-turn /usr/bin/
ADD e2e/coturn-client/client.sh /usr/bin