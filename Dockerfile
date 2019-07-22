FROM golang:latest

# Use only for development.
EXPOSE 3478:3478/udp
COPY . /go/src/gortc.io/gortcd
RUN go install gortc.io/gortcd

ENTRYPOINT ["/go/bin/gortcd"]
