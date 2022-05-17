# Contributing guidelines

## Issues

Before filing a new issue, check if it has already been reported and vote with a :+1: if it has. If the existing issue is a bug,
and its description doesn't include your specific observed behavior or your environment, add a comment.

Feature requests:
- please keep an eye on the issue after filing it as we will likely be reaching out with questions.

Bug reports:
- try to come up with the simpliest case that illustrates the bug
- provide as many details as possible:
    - expected result
    - observed result
    - your configuration
    - your environment (Kubernetes and NetBox versions)

## Pull Requests

Before creating any pull requests, please file an issue: it will allow other users to track any known problems.
- for any code changes, add unit tests or explain why they are not necessary
- ensure your changes pass CI: if they don't, the PR will be considered incomplete and won't be reviewed
- try to keep PRs small, if a large PR can be split into smaller ones, it's generally better to do so
