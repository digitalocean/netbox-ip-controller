#!/bin/bash

####################################################################
# Runs integration tests for netbox-ip-controller.
####################################################################

set -euo pipefail

POSTGRES_IMAGE=postgres:14
REDIS_IMAGE=redis:6
NETBOX_IMAGE=netboxcommunity/netbox:v3.1

POSTGRES_USER=netbox
POSTGRES_DATABASE=netbox
POSTGRES_PASSWORD=netbox
NETBOX_SECRET_KEY='!$9qmU@9qixPE6Qn*mfw94tOolJdkEa#e8F456e17NviB5qlnk'
NETBOX_SUPERUSER_API_TOKEN='48c7ba92-0f82-443a-8cf3-981559ff32cf'

CTHULHU_DIR=${CTHULHU_DIR:-}
if [ -z "${CTHULHU_DIR}" ]; then
  printf "CTHULHU_DIR is not set in your environment\n" >&2
  exit 1
fi

# cleanup function that is called when we exit via or the script is finished;
# need to declare this now so it can be catched later during the script
function cleanup {
  printf "==> stopping containers\n" >&2
  docker stop netbox-postgres
  docker stop netbox-redis
  docker stop netbox

  printf "==> removing network\n" >&2
  docker network remove netbox-integration
}
trap cleanup INT EXIT


printf "==> creating network\n" >&2
docker network create netbox-integration

printf "==> starting postgres\n" >&2
docker run \
  --rm \
  --detach \
  --network netbox-integration \
  --name netbox-postgres \
  --hostname netbox-postgres \
  --env POSTGRES_USER=${POSTGRES_USER} \
  --env POSTGRES_DATABASE=${POSTGRES_DATABASE} \
  --env POSTGRES_PASSWORD=${POSTGRES_PASSWORD} \
  ${POSTGRES_IMAGE}

printf "==> starting redis\n" >&2
docker run \
  --rm \
  --detach \
  --network netbox-integration \
  --name netbox-redis \
  --hostname netbox-redis \
  --env ALLOW_EMPTY_PASSWORD=yes \
  ${REDIS_IMAGE}

printf "==> starting netbox\n" >&2
docker run \
  --rm \
  --detach \
  --network netbox-integration \
  --name netbox \
  --hostname netbox \
  --publish 8080:8080 \
  --env ALLOWED_HOSTS='*' \
  --env REDIS_HOST=netbox-redis.netbox-integration \
  --env REDIS_PORT=6379 \
  --env REDIS_DATABASE=0 \
  --env REDIS_CACHE_HOST=netbox-redis \
  --env REDIS_CACHE_PORT=6379 \
  --env REDIS_CACHE_DATABASE=1 \
  --env DB_HOST=netbox-postgres.netbox-integration \
  --env DB_NAME=${POSTGRES_DATABASE} \
  --env DB_USER=${POSTGRES_USER} \
  --env DB_PASSWORD=${POSTGRES_PASSWORD} \
  --env SECRET_KEY=${NETBOX_SECRET_KEY} \
  --env SUPERUSER_API_TOKEN=${NETBOX_SUPERUSER_API_TOKEN} \
  ${NETBOX_IMAGE}

printf "==> waiting for netbox to become ready" >&2
interval_sec=5
max_attempts=40
attempt=0
until [[ $(curl --silent --output /dev/null --write-out "%{http_code}" http://127.0.0.1:8080) == "200" ]]; do
  attempt=$((attempt+1))
  if (("${attempt}" == "${max_attempts}")); then
    printf "==> attempts exhausted waiting for netbox\n" >&2
    exit 1
  fi
  printf "." >&2
  sleep "${interval_sec}"
done
printf "\n" >&2

printf "==> running tests\n" >&2
docker run \
  --env GOFLAGS=-mod=vendor \
  --env GOCACHE=/cthulhu/.gocache \
  --volume "${CTHULHU_DIR}":/cthulhu \
  --workdir /cthulhu/docode/src/do \
  --rm --interactive --tty docker.internal.digitalocean.com/platcore/envtest:e7355a795b \
  go test -tags="integration sandbox" do/teams/delivery/netbox-ip-controller/cmd/netbox-ip-controller
