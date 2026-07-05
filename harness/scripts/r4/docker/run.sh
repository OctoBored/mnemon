#!/usr/bin/env bash
# R4 docker double-node smoke driver (host side): builds the image and runs it.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
if ! docker info >/dev/null 2>&1; then
	echo "docker unavailable — skipping docker double-node smoke (gate treats this as SKIP, not FAIL)"
	exit 0
fi
docker build -q -f harness/scripts/r4/docker/Dockerfile -t mnemon-r4-docker . >/dev/null
docker run --rm mnemon-r4-docker
