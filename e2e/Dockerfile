ARG CI_GO_VERSION
FROM golang:${CI_GO_VERSION}
ADD vendor /go/src/github.com/gortc/gortcd/e2e/vendor
WORKDIR /go/src/github.com/gortc/gortcd/e2e/
ADD main.go .
RUN go build .

FROM yukinying/chrome-headless-browser
COPY --from=0 /go/src/github.com/gortc/gortcd/e2e/e2e .
COPY static static
ENTRYPOINT ["./e2e", "-b=/usr/bin/google-chrome-unstable", "-timeout=3s"]
