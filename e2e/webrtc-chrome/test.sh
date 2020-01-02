#!/usr/bin/env bash

export CURRENT_GO_VERSION=$(echo -n "$(go version)" | grep -o 'go1\.[0-9|\.]*' || true)
CURRENT_GO_VERSION=${CURRENT_GO_VERSION#go}
GO_VERSION=${GO_VERSION:-$CURRENT_GO_VERSION}

# set golang version from env
export CI_GO_VERSION="${GO_VERSION:-latest}"

# define some colors to use for output
RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

printf "${GREEN}Go version \"${CI_GO_VERSION}\"${NC}\n"

# kill and remove any running containers
cleanup () {
  docker stop ci_gortcd_tcpdump
  docker rm -f ci_gortcd_tcpdump
  docker-compose --no-ansi -p ci kill
  docker-compose --no-ansi -p ci rm -f
  docker network rm gortcd_e2e_webrtc
}

# catch unexpected failures, do cleanup and output an error message
trap 'cleanup ; printf "${RED}Tests Failed For Unexpected Reasons${NC}\n"'\
  HUP INT QUIT PIPE TERM

# PREPARING NETWORK CAPTURE
docker network create gortcd_e2e_webrtc --internal
docker build -t gortc/tcpdump -f tcpdump.Dockerfile .

NETWORK_ID=`docker network inspect gortcd_e2e_webrtc -f "{{.Id}}"`
NETWORK_SUBNET=`docker network inspect gortcd_e2e_webrtc -f "{{(index .IPAM.Config 0).Subnet}}"`
CAPTURE_INTERFACE="br-${NETWORK_ID:0:12}"

echo "will capture traffic on $CAPTURE_INTERFACE$"

docker rm -f ci_gortcd_tcpdump
docker run -e INTERFACE=${CAPTURE_INTERFACE} -e SUBNET=${NETWORK_SUBNET} -d \
    -v $(pwd):/root/dump \
    --name ci_gortcd_tcpdump --net=host gortc/tcpdump


# build and run the composed services
docker-compose --no-ansi -p ci build && docker-compose --no-ansi -p ci up --remove-orphans -d
if [ $? -ne 0 ] ; then
  printf "${RED}Docker Compose Failed${NC}\n"
  exit -1
fi

# wait for the test service to complete and grab the exit code
TEST_EXIT_CODE=`docker wait ci_turn-controlling_1`

# output the logs for the test (for clarity)
docker logs ci_turn-controlling_1 &> log-controlling.txt
docker logs ci_turn-controlled_1 &> log-controlled.txt
docker logs ci_turn-server_1 &> log-server.txt
docker logs ci_gortcd_tcpdump &> log-tcpdump.txt
docker logs ci_signaling_1 &> log-signaling.txt

cat log-controlling.txt

# inspect the output of the test and display respective message
if [ -z ${TEST_EXIT_CODE+x} ] || [ "$TEST_EXIT_CODE" -ne 0 ] ; then
  printf "${RED}Tests Failed${NC} - Exit Code: $TEST_EXIT_CODE\n"
  printf "${GREEN}Logs from turn server:${NC}\n"
  cat log-server.txt
else
  printf "${GREEN}Tests Passed${NC}\n"
fi

# call the cleanup function
cleanup

# exit the script with the same code as the test service code
exit ${TEST_EXIT_CODE}
