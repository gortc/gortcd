ARG CI_GO_VERSION
FROM golang:${CI_GO_VERSION}

ADD go.mod /src/turn-client/
ADD go.sum /src/turn-client/
WORKDIR /src/turn-client/
RUN go mod download

ADD main.go /src/turn-client/

RUN go install .

CMD ["turn-client"]
