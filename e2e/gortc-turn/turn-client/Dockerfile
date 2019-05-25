ARG CI_GO_VERSION
FROM golang:${CI_GO_VERSION}

ADD . /src/turn-client
WORKDIR /src/turn-client
RUN go install .

CMD ["turn-client"]
