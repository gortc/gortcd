[![codecov](https://codecov.io/gh/gortc/gortcd/branch/master/graph/badge.svg)](https://codecov.io/gh/gortc/gortcd)
[![Master status](https://tc.gortc.io/app/rest/builds/buildType:(id:gortcd_MasterStatus)/statusIcon.svg)](https://tc.gortc.io/project.html?projectId=gortcd&tab=projectOverview&guest=1)
[![Build Status](https://travis-ci.com/gortc/gortcd.svg?branch=master)](https://travis-ci.com/gortc/gortcd)
[![stability-wip](https://img.shields.io/badge/stability-wip-lightgrey.svg)](https://github.com/mkenney/software-guides/blob/master/STABILITY-BADGES.md#work-in-progress)
[![GitHub release](https://img.shields.io/github/release/gortc/gortcd.svg)](https://github.com/gortc/gortcd/releases/latest)
# gortcd
The gortcd is work-in-progress TURN and STUN server implementation in go.
As part of [gortc](https://gortc.io) project, gortcd shares
it's [goals](https://gortc.io#goals) and
[principles](https://gortc.io#principles).
Based on [gortc/stun](https://github.com/gortc/stun) package.

The goal is [feature parity](https://github.com/gortc/gortcd/issues/6) with [coturn](https://github.com/coturn/coturn).
Please use only for experiments until [beta](https://github.com/gortc/gortcd/milestone/2).


# Install
See [releases](https://github.com/gortc/gortcd/releases/latest) for latest
binaries and packages or [snapshot](https://tc.gortc.io/viewType.html?buildTypeId=gortcd_snapshot&guest=1)
artifacts for bleeding-edge ones.

## Configuration
Please see `gortc.yml` for configuration tips. Server listens on all
available interfaces by default, STUN is public, TURN is private and
no valid credentials are provided.

Server searches for `gortc.yml` in current directory, in the
`/etc/gortcd/` and in home directory.
```yml
auth:
# Put here valid credentials.
# So, if you are passing to RTCPeerConnection 
# something like that: 
#  {
#    urls: "turn:turnserver.example.org",
#    username: "webrtc",
#    credential: "turnpassword"
#  }
# Use the following:
  static:
    - username: webrtc
      password: turnpassword
```
If you want TURN without auth, set `auth.public` to `true`.

## Docker

[![Docker Pulls](https://img.shields.io/docker/pulls/gortc/gortcd.svg)](https://hub.docker.com/r/gortc/gortcd/)

The gortcd docker image is automatically built on every release from
the `release.Dockerfile` which is based on `scratch`. Also each release
is available as separate tagged image, e.g. `gortc/gortcd:v0.5.1`.

```bash
# Run with default config:
$ docker run --name turn -d -p 3478:3478/udp gortc/gortcd

# You can supply custom config file, for example `gortcd.yml` 
# from current directory:
$ docker run --name turn -d -p 3478:3478/udp \
  -v $(pwd)/gortcd.yml:/etc/gortc/gortc.yml \
  gortc/gortcd --config /etc/gortc/gortc.yml
```

# Supported specifications

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

# Testing

Server behavior is tested and verified in many ways:
  * End-To-End with long-term credentials
    * **webrtc**: Two browsers using gortcd as relay for WebRTC data channels (linux)
    * **gortc**: The [gortc/turn](https://github.com/gortc/turn) client (windows)
    * **coturn**: The coturn [uclient](https://github.com/coturn/coturn/wiki/turnutils_uclient) (linux)
  * Bunch of code static checkers (linters)
  * Standard unit-tests with coverage reporting

See [TeamCity project](https://tc.gortc.io/project.html?projectId=gortcd&guest=1) and `e2e` directory
for more information. Also the Wireshark `.pcap` files are available for some of e2e tests in
artifacts for build.

# Artifact origin verification
Each release is signed with [PGP key](https://keybase.io/ernado) `1D14 A82D 2E31 1045`.
```bash
$ gpg --keyserver keyserver.ubuntu.com --recv 2E311045
$ gpg --decrypt gortcd-*-checksums.txt.sig

# to check gortcd-*-linux-amd64.deb:
$ grep -F "$(sha256sum gortcd-*-linux-amd64.deb)" gortcd-*-checksums.txt
4316f8f7b66bdba636a991198701914e12d11935748547fca1d97386808ce323  gortcd-0.4.0-linux-amd64.deb
```