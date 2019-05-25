ARG CI_GO_VERSION
FROM golang:${CI_GO_VERSION}

ADD go.mod /src/e2e/
ADD go.sum /src/e2e/
WORKDIR /src/e2e/
RUN go mod download

ADD main.go /src/e2e/
RUN go build -o /root/e2e

FROM yukinying/chrome-headless-browser
COPY --from=0 /root/e2e .
COPY static static
ENTRYPOINT ["./e2e", "-b=/usr/bin/google-chrome-unstable", "-timeout=3s"]
