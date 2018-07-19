[![codecov](https://codecov.io/gh/gortc/gortcd/branch/master/graph/badge.svg)](https://codecov.io/gh/gortc/gortcd)
[![Build Status](https://travis-ci.com/gortc/gortcd.svg?branch=master)](https://travis-ci.com/gortc/gortcd)

# gortcd
The gortcd is work-in-progress TURN and STUN server implementation in go.
As part of [gortc](https://gortc.io) project, gortcd shares
it's [goals](https://github.com/gortc/dev#goals) and
[principles](https://github.com/gortc/dev#principles).
Based on [gortc/stun](https://github.com/gortc/stun) package.

The goal is feature parity with [coturn](https://github.com/coturn/coturn).
Please use only for experiments until [beta](https://github.com/gortc/gortcd/milestone/2).


# Install
See [releases](https://github.com/gortc/gortcd/releases/latest) for latest
binaries and packages.
## Docker
```
docker run -d -p 3478:3478/udp gortc/gortcd
```

# Verify
```bash
$ gpg --keyserver keyserver.ubuntu.com --recv 2E311045
$ gpg --decrypt gortcd-*-checksums.txt.sig

# to check gortcd-*-linux-amd64.deb:
$ grep -F "$(sha256sum gortcd-*-linux-amd64.deb)" gortcd-*-checksums.txt
4316f8f7b66bdba636a991198701914e12d11935748547fca1d97386808ce323  gortcd-0.4.0-linux-amd64.deb
```