[![codecov](https://codecov.io/gh/gortc/gortcd/branch/master/graph/badge.svg)](https://codecov.io/gh/gortc/gortcd)
[![Build Status](https://travis-ci.com/gortc/gortcd.svg?branch=master)](https://travis-ci.com/gortc/gortcd)
[![stability-wip](https://img.shields.io/badge/stability-wip-lightgrey.svg)](https://github.com/mkenney/software-guides/blob/master/STABILITY-BADGES.md#work-in-progress)
# gortcd
The gortcd is work-in-progress TURN and STUN server implementation in go.
As part of [gortc](https://gortc.io) project, gortcd shares
it's [goals](https://gortc.io#goals) and
[principles](https://gortc.io#principles).
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

# Supported RFC's

TURN specs:

  * RFC 5766 - base TURN specs

STUN specs:

  * RFC 5389 - base "new" STUN specs
  * RFC 5769 - test vectors for STUN protocol testing


The implementation fully supports the following client-to-TURN-server protocols:

  * UDP (per RFC 5766)


Supported relay protocols:

  * UDP (per RFC 5766)

Supported message integrity digest algorithms:

  * HMAC-SHA1, with MD5-hashed keys (as required by STUN and TURN standards)

Supported TURN authentication mechanisms:

  * 'classic' long-term credentials mechanism;

Project supports all platforms that [supports](https://github.com/golang/go/wiki/MinimumRequirements#minimum-requirements) go.