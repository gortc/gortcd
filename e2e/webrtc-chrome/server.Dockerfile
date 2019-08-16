ARG CI_GO_VERSION
FROM golang:${CI_GO_VERSION}

ADD go.mod go.sum /src/gortcd/
WORKDIR /src/gortcd/
RUN go mod download

ADD main.go /src/gortcd/
ADD internal /src/gortcd/internal

RUN go install .
COPY e2e/webrtc-chrome/gortcd.yml .

CMD ["gortcd"]
