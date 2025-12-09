#!/bin/bash

# Note: The STABLE_ prefix will force a relink when the value changes when using rules_go x_defs.

# Ensure PATH includes common tool locations for both Linux and macOS
# This is needed because Bazel's workspace_status_command runs in a restricted environment
export PATH="/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:$PATH"

echo STABLE_GIT_COMMIT "$(git rev-parse HEAD)"

# Use portable date format compatible with both GNU (Linux) and BSD (macOS) date
if date --version >/dev/null 2>&1; then
    # GNU date (Linux)
    echo DATE "$(date --rfc-3339=seconds --utc)"
    echo DATE_UNIX "$(date --utc +%s)"
else
    # BSD date (macOS)
    echo DATE "$(date -u +"%Y-%m-%d %H:%M:%S+00:00")"
    echo DATE_UNIX "$(date -u +%s)"
fi

echo DOCKER_TAG "$(git rev-parse --abbrev-ref HEAD)-$(git rev-parse --short=6 HEAD)"
echo STABLE_GIT_TAG "$(git describe --tags --abbrev=0)"
