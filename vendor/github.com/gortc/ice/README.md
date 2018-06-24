[![Build Status](https://travis-ci.com/gortc/ice.svg)](https://travis-ci.com/gortc/ice)
[![GoDoc](https://godoc.org/github.com/gortc/ice?status.svg)](http://godoc.org/github.com/gortc/ice)
[![Coverage Status](https://coveralls.io/repos/github/gortc/ice/badge.svg?branch=master)](https://coveralls.io/github/gortc/ice?branch=master)
[![Go Report](https://goreportcard.com/badge/github.com/gortc/ice)](http://goreportcard.com/report/gortc/ice)

# ICE
Package ice implements Interactive Connectivity Establishment (ICE): A Protocol for Network Address Translator (NAT) Traversal for Offer/Answer Protocols.

Currently in active development. Do not use this package at all. API will
definitely break. 

Needs go 1.7 or better.

## RFCs

The package aims to implement the follwing RFCs. Note that the requirement status is based on the [WebRTC spec](https://tools.ietf.org/html/draft-ietf-rtcweb-overview), focusing on data channels for now.

rfc | status | requirement | description
----|--------|-------------|----
[![RFC5245Bis](https://img.shields.io/badge/RFC-5766Bis-blue.svg)](https://tools.ietf.org/html/draft-ietf-ice-rfc5245bis) | ![status](https://img.shields.io/badge/status-dev-blue.svg) | [![status](https://img.shields.io/badge/requirement-MUST-green.svg)](https://tools.ietf.org/html/rfc2119) | Interactive Connectivity Establishment
[IPv6](https://tools.ietf.org/html/draft-ietf-rtcweb-transports#section-3.1) | ![status](https://img.shields.io/badge/status-research-orange.svg) | [![status](https://img.shields.io/badge/requirement-MUST-green.svg)](https://tools.ietf.org/html/rfc2119) | IPv6 support
[![RFC6544](https://img.shields.io/badge/RFC-6544-blue.svg)](https://tools.ietf.org/html/rfc6544) | ![status](https://img.shields.io/badge/status-research-orange.svg) | [![status](https://img.shields.io/badge/requirement-MUST-green.svg)](https://tools.ietf.org/html/rfc2119) | ICE-TCP candidates
[Trickle ICE](https://tools.ietf.org/html/draft-ietf-ice-trickle) | ![status](https://img.shields.io/badge/status-research-orange.svg) | [![status](https://img.shields.io/badge/requirement-MUST-green.svg)](https://tools.ietf.org/html/rfc2119) | Incremental Provisioning of Candidates for the ICE
[Dual Stack Fairness](https://tools.ietf.org/html/draft-ietf-mmusic-ice-dualstack-fairness) | ![status](https://img.shields.io/badge/status-research-orange.svg) | [![status](https://img.shields.io/badge/requirement-SHOULD-blue.svg)](https://tools.ietf.org/html/rfc2119) | ICE Multihomed and IPv4/IPv6 Dual Stack Fairness
