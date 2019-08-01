[![Master status](https://tc.gortc.io/app/rest/builds/buildType:(id:gortcd_MasterStatus)/statusIcon.svg)](https://tc.gortc.io/project.html?projectId=gortcd&tab=projectOverview&guest=1)
[![codecov](https://codecov.io/gh/gortc/gortcd/branch/master/graph/badge.svg)](https://codecov.io/gh/gortc/gortcd)
[![GitHub release](https://img.shields.io/github/release/gortc/gortcd.svg)](https://github.com/gortc/gortcd/releases/latest)
# gortcd
The gortcd is work-in-progress TURN [[RFC5776](https://tools.ietf.org/html/rfc5766)] and STUN [[RFC5389](https://tools.ietf.org/html/rfc5389)] server implementation in go.
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

## PIE version
Note that `gortcd-*-linux-arm64.tar.gz` archive also contains the
`gortcd-pie` binary which is [position-independent](https://en.wikipedia.org/wiki/Position-independent_code)
executable version. The `gortcd-pie` is installed with `gortcd-*-linux-arm64.deb`
too, but not used by default.

## Configuration
Please see [gortc.yml](https://github.com/gortc/gortcd/blob/master/gortcd.yml)
for configuration tips. Server listens on all available interfaces by default,
STUN is public, TURN is private and no credentials are valid (nobody can't auth).
Send `SIGUSR2` to reload config or use `gortcd reload` command (not all
options support live config reload).

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
[![](https://images.microbadger.com/badges/image/gortc/gortcd.svg)](https://microbadger.com/images/gortc/gortcd "Get your own image badge on microbadger.com")
[![](https://images.microbadger.com/badges/version/gortc/gortcd.svg)](https://microbadger.com/images/gortc/gortcd "Get your own version badge on microbadger.com")
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
  
# To allow gortcd to listen directly on your public interface instead
# of using docker port publishing, pass --net=host to docker run.
$ docker run --name turn --net=host -d -p 3478:3478/udp  
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

# Benchmarks

Currently server is under active development, but some benchmarks are
already available. The [stun-bench](https://github.com/pion/stun/blob/master/cmd/stun-bench/main.go)
is simple stun benchmark that performs binding request transactions.

Results for gortcd v0.17.4 on Ubuntu 16.04, client and server share one
machine with Intel 8700k CPU:
```bash
$ ./stun-bench -w 50 -d 5s
workers started
rps: 580606
total: 2903188
```
The memory consumption was constant `13 348kb`.

Just to compare, the coturn:
```bash
$ ./stun-bench -w 50 -d 5s
workers started
rps: 627709
total: 3138656
```
The memory consumption was constant `15 068kb`.

Please interpret results carefully, the coturn server is much more
functional.

# Testing

Server behavior is tested and verified in many ways:
  * End-To-End with long-term credentials
    * **webrtc**: Two browsers using gortcd as relay for WebRTC data channels (linux)
    * **gortc**: The [gortc/turn](https://github.com/gortc/turn) client (windows)
    * **coturn**: The coturn [uclient](https://github.com/coturn/coturn/wiki/turnutils_uclient) (linux)
  * Bunch of code static checkers (linters)
  * Standard unit-tests with coverage reporting (linux {amd64, **arm**64}, windows)

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

# Monitoring
You can export metrics in prometheus format:
```yaml
server:
  prometheus:
    addr: localhost:9200
```
```bash
$ curl http://localhost:9200/metrics
```
```bash
# HELP gortcd_allocation_count Total number of allocations.
# TYPE gortcd_allocation_count gauge
gortcd_allocation_count{addr="159.69.47.227:3478"} 0
# HELP gortcd_binding_count Total number of bindings.
# TYPE gortcd_binding_count gauge
gortcd_binding_count{addr="159.69.47.227:3478"} 0
# HELP gortcd_permission_count Total number of permissions.
# TYPE gortcd_permission_count gauge
gortcd_permission_count{addr="159.69.47.227:3478"} 0
```

## Build status
[![Build Status](https://travis-ci.com/gortc/gortcd.svg?branch=master)](https://travis-ci.com/gortc/gortcd)

## License
[![FOSSA Status](https://app.fossa.io/api/projects/git%2Bgithub.com%2Fgortc%2Fgortcd.svg?type=large)](https://app.fossa.io/projects/git%2Bgithub.com%2Fgortc%2Fgortcd?ref=badge_large)