FROM golang:latest

# Use only for development.
EXPOSE 3478:3478/udp
COPY . /go/src/github.com/gortc/gortcd
RUN go install github.com/gortc/gortcd

ENTRYPOINT ["/go/bin/gortcd"]
