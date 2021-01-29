#!/bin/sh

# This wraps buildkitd and allows the container to exit gracefully with exit code 0,
# so that the Kubernetes Job is recognized as successful and doesn't get retried

rootlesskit buildkitd "$@"

exit 0
