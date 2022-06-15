#!/bin/bash

check=$(staticcheck -checks=inherit,ST1020,ST1021,ST1022 ./... | grep -v "vendor/" )

if [[ -n $check ]]; then
    echo "static check issues found"
    IFS=$'\n'

    for line in $check; do
        echo "staticcheck: $line"
    done

    exit 1
fi

echo "no issues found"

exit 0