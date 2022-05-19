#!/bin/bash

check=$(staticcheck ./... | grep -v "vendor/" )

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