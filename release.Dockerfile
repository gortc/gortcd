FROM scratch
EXPOSE 3478:3478/udp
COPY gortcd /
COPY gortcd.yml /
ENTRYPOINT ["/gortcd"]
