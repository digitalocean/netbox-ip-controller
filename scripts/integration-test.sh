#!/bin/bash

set -euo pipefail

# Setup, execute, and cleanup test
source scripts/integration-test-functions.sh
trap cleanup_test EXIT
setup_test
CONTROLLER_DIR="$(pwd)" execute_test