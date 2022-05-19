#!/bin/bash

set -euo pipefail

# Cleanup function to be called when the script finishes/gets cancelled.
function cleanup_test {

  printf "==> stopping containers\n" >&2

  for c in netbox-postgres netbox-redis netbox 
  do 
    if [[  -n  $(docker container ls --filter "name=$c" -q) ]]; then
      docker stop $c
    fi
  done

  printf "==> removing network\n" >&2
  
  if [[  -n  $(docker network ls --filter "name=netbox-integration" -q) ]]; then
    docker network remove netbox-integration
  fi
}

# Sets up postgres, redis, and netbox in docker containers to be used by the integrationt test
function setup_test {

  POSTGRES_IMAGE=postgres:14
  REDIS_IMAGE=redis:6
  NETBOX_IMAGE=netboxcommunity/netbox:v3.1

  POSTGRES_USER=netbox
  POSTGRES_DATABASE=netbox
  POSTGRES_PASSWORD=netbox
  NETBOX_SECRET_KEY='!$9qmU@9qixPE6Qn*mfw94tOolJdkEa#e8F456e17NviB5qlnk'
  NETBOX_SUPERUSER_API_TOKEN='48c7ba92-0f82-443a-8cf3-981559ff32cf'

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
    --env REDIS_CACHE_HOST=netbox-redis.netbox-integration \
    --env REDIS_CACHE_PORT=6379 \
    --env REDIS_CACHE_DATABASE=1 \
    --env DB_HOST=netbox-postgres.netbox-integration \
    --env DB_NAME=${POSTGRES_DATABASE} \
    --env DB_USER=${POSTGRES_USER} \
    --env DB_PASSWORD=${POSTGRES_PASSWORD} \
    --env SECRET_KEY=${NETBOX_SECRET_KEY} \
    --env SUPERUSER_API_TOKEN=${NETBOX_SUPERUSER_API_TOKEN} \
    --env EXEMPT_VIEW_PERMISSIONS="*" \
    ${NETBOX_IMAGE}

  printf "==> waiting for netbox to become ready" >&2
  interval_sec=5
  max_attempts=100
  attempt=0
  until [[ $(curl --silent --output /dev/null --write-out "%{http_code}" http://127.0.0.1:8080) == "200" ]]; do
    attempt=$((attempt+1))
    if ((${attempt} == ${max_attempts})); then
      printf "==> attempts exhausted waiting for netbox\n" >&2
      exit 1
    fi
    printf "." >&2
    sleep "${interval_sec}"
  done
  printf "\n" >&2
}

# CONTROLLER_DIR must be set to the path to the netbox-ip-controller repository. 
# Runs the integration test
function execute_test {

  if [[ -z "${CONTROLLER_DIR:-}" ]]; then
    printf "error: CONTROLLER_DIR must be set to the path to the netbox-ip-controller repo\n" >&2
    exit 1
  fi

  printf "==> running tests\n" >&2

  docker run \
    --env GOFLAGS=-mod=vendor \
    --network netbox-integration \
    --volume "${CONTROLLER_DIR}":/netbox-ip-controller \
    --workdir /netbox-ip-controller \
    --rm --interactive docker.internal.digitalocean.com/platcore/envtest:b2d9f74c85 \
    go test -tags="integration sandbox" github.com/digitalocean/netbox-ip-controller/cmd/netbox-ip-controller "$@"
}
