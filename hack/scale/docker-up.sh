#!/usr/bin/env bash
# Start N tiny HTTP containers (traefik/whoami, under 1MB RSS each) that graph-go's
# Docker discovery picks up as http services. Use with a config that has
# docker discovery enabled (e.g. the default one) to exercise many containers
# and container churn.
#
# Usage: hack/scale/docker-up.sh [count]   (default 50)
set -euo pipefail
COUNT=${1:-50}
IMAGE=${IMAGE:-traefik/whoami:v1.10}

docker pull -q "$IMAGE" >/dev/null
for i in $(seq 1 "$COUNT"); do
  name=$(printf 'scale-web-%03d' "$i")
  state=$(docker inspect -f '{{index .Config.Labels "graphgo.scale"}} {{.State.Running}}' "$name" 2>/dev/null || true)
  case $state in
    "1 true") continue ;;
    "1 false") docker start "$name" >/dev/null; continue ;;
    "") ;; # free name
    *) echo "refusing to reuse $name: it exists but is not a scale container" >&2; exit 1 ;;
  esac
  docker run -d --name "$name" --label graphgo.scale=1 \
    -e WHOAMI_PORT_NUMBER=8080 --memory 32m "$IMAGE" >/dev/null
done
echo "$(docker ps -q --filter label=graphgo.scale=1 | wc -l) scale containers running."
