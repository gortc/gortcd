ARG CI_GO_VERSION
FROM golang:${CI_GO_VERSION}

ADD go.mod /src/stun-client/
ADD go.sum /src/stun-client/
WORKDIR /src/stun-client/
RUN go mod download

ADD main.go /src/stun-client/

RUN go install .

CMD ["stun-client"]
