#!/usr/bin/env bash

./usr/bin/wait-turn

./usr/bin/turnutils_uclient -u username -w secret \
 -L coturn-client -e coturn-peer turn-server