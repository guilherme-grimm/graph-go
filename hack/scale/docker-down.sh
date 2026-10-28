#!/usr/bin/env bash
# Remove the containers started by docker-up.sh.
set -euo pipefail
ids=$(docker ps -aq --filter label=graphgo.scale=1)
[[ -n $ids ]] && docker rm -f $ids >/dev/null
echo "Removed scale containers."
