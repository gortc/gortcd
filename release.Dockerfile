FROM scratch
EXPOSE 3478:3478/udp
COPY gortcd /
ENTRYPOINT ["/gortcd"]
