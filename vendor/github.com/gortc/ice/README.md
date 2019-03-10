[![Build Status](https://travis-ci.com/gortc/ice.svg)](https://travis-ci.com/gortc/ice)
[![Master status](https://tc.gortc.io/app/rest/builds/buildType:(id:ice_MasterStatus)/statusIcon.svg)](https://tc.gortc.io/project.html?projectId=ice&tab=projectOverview&guest=1)
[![GoDoc](https://godoc.org/github.com/gortc/ice?status.svg)](http://godoc.org/github.com/gortc/ice)
[![codecov](https://codecov.io/gh/gortc/ice/branch/master/graph/badge.svg)](https://codecov.io/gh/gortc/ice)
[![Go Report](https://goreportcard.com/badge/github.com/gortc/ice)](http://goreportcard.com/report/gortc/ice)
[![stability-wip](https://img.shields.io/badge/stability-wip-lightgrey.svg)](https://github.com/mkenney/software-guides/blob/master/STABILITY-BADGES.md#work-in-progress)
[![FOSSA Status](https://app.fossa.io/api/projects/git%2Bgithub.com%2Fgortc%2Fice.svg?type=shield)](https://app.fossa.io/projects/git%2Bgithub.com%2Fgortc%2Fice?ref=badge_shield)
# ICE
Package ice implements Interactive Connectivity Establishment (ICE):
A Protocol for Network Address Translator (NAT) Traversal for Offer/Answer Protocols
[[RFC 8445](https://tools.ietf.org/html/rfc8445)].
Complies to [gortc principles](https://gortc.io/#principles) as core package.

Currently in active development, so no guarantees for API backward
compatibility.

## Supported RFCs
- [ ] [RFC 8445](https://tools.ietf.org/html/rfc8445) — Interactive Connectivity Establishment
    - [ ] Basic
    - [ ] Full
    - [ ] [Trickle](https://tools.ietf.org/html/draft-ietf-ice-trickle)
- [ ] [RFC 6544](https://tools.ietf.org/html/draft-ietf-ice-rfc5245bis) — TCP Candidates with ICE
- [ ] [rtcweb-19](https://tools.ietf.org/html/draft-ietf-rtcweb-overview-19) — WebRTC
    - [ ] [rtcweb-transports-17](https://tools.ietf.org/html/draft-ietf-rtcweb-transports-17) — Transports

## License
[![FOSSA Status](https://app.fossa.io/api/projects/git%2Bgithub.com%2Fgortc%2Fice.svg?type=large)](https://app.fossa.io/projects/git%2Bgithub.com%2Fgortc%2Fice?ref=badge_large)