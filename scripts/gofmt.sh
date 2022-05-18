#!/bin/bash

fmt=$(gofmt -l . | grep -v "vendor/")
if [[ -n $fmt ]]; then
    echo "gofmt detected the following issues. Please run:"
    for line in $fmt; do
        printf "\n  gofmt -w $line\n\n"
        gofmt -d $line
    done
    exit 1
fi

echo "no issues found"
exit 0