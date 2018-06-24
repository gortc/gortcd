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