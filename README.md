[![Coverage Status](https://coveralls.io/repos/github/gortc/gortcd/badge.svg)](https://coveralls.io/github/gortc/gortcd)
[![Build Status](https://travis-ci.com/gortc/gortcd.svg?branch=master)](https://travis-ci.com/gortc/gortcd)

# gortcd
WIP TURN and STUN server in go

# Description
Work in progress STUN and TURN server implementation.
Goal is feature parity with coturn and more.

Currently in active development, use only for experiments.

# Install
## Docker
```
docker run -d -p 3478:3478/udp gortc/gortcd
```

# Verify
```bash
$ gpg --keyserver keyserver.ubuntu.com --recv 2E311045
$ gpg --decrypt gortcd_*_checksums.txt.sig

# to check gortcd-*-linux-amd64.deb:
$ grep -F "$(sha256sum gortcd-*-linux-amd64.deb)" gortcd_*_checksums.txt
4316f8f7b66bdba636a991198701914e12d11935748547fca1d97386808ce323  gortcd-0.4.0-linux-amd64.deb
```