FROM scratch
EXPOSE 3478:3478/udp
COPY gortcd /usr/bin/gortcd
COPY gortcd.yml /etc/gortcd/gortcd.yml
ENTRYPOINT ["/usr/bin/gortcd"]
