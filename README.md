# gortcd
WIP TURN and STUN server in go

# Description
Server is RFC 5389 basic server implementation.

Current implementation is UDP only and not utilizes FINGERPRINT mechanism,
nor ALTERNATE-SERVER, nor credentials mechanisms. It does not support
backwards compatibility with RFC 3489

# Install
## Docker
```
docker run -d -p 3478:3478 gortc/gortcd
```