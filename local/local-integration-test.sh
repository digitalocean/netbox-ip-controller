#!/bin/bash

# don't call this script directly; use the Makefile in the root folder instead

set -euo pipefail

source scripts/integration-test-functions.sh

if [[ $1 = "all" ]]; then 
    trap cleanup_test EXIT
    setup_test
    CONTROLLER_DIR="$(pwd)" execute_test
elif [[ $1 = "setup" ]]; then
    setup_test
elif [[ $1 = "execute" ]]; then
    CONTROLLER_DIR="$(pwd)" execute_test
elif [[ $1 = "cleanup" ]]; then
    cleanup_test
fi
