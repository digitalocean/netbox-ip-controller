#!/bin/bash

# If the Makefile or test/Dockerfile have been changed against the head of the master
# branch, re-build the testenv image and set TEST_IMAGE to its tag

curr=$(git rev-parse HEAD)
git checkout master
changed=$(git diff --name-only HEAD $curr)
git checkout $curr

while IFS= read -r file; do

    if [[ $file == "test/Dockerfile" || $file == "Makefile" ]]; then
        echo "Change found. Re-building testenv image"
        make envtest-image
        image=$(docker images --format {{.Repository}}:{{.Tag}} | head -n1)
        echo "TEST_IMAGE=$image" >> $GITHUB_ENV
        printf "Using testenv image $image" >&2
        break
    fi

done <<< "$changed"
