#!/bin/bash
set -euo pipefail

echo "Running tests for packages github.com/digitalocean/netbox-ip-controller"

go test -cover -race ./...

echo "Finished running tests"
