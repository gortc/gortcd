ARG CI_GO_VERSION
FROM golang:${CI_GO_VERSION}
ADD go.mod /src/signaling/
ADD go.sum /src/signaling/
WORKDIR /src/signaling/
RUN go mod download

ADD main.go /src/signaling/

RUN go build -o /usr/bin/signaling
CMD ["signaling"]
