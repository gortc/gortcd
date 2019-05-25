FROM golang:1.12

ADD go.mod /src/gortcd/
ADD go.sum /src/gortcd/
WORKDIR /src/gortcd/
RUN go mod download

ADD main.go /src/gortcd/
ADD internal /src/gortcd/internal

RUN go install .
COPY e2e/gortc-turn/gortcd.yml .

CMD ["gortcd"]
