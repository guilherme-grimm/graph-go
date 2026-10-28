#!/usr/bin/env bash
# Start N tiny HTTP containers (traefik/whoami, ~5MB RSS each) that graph-go's
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
  docker inspect "$name" >/dev/null 2>&1 && continue
  docker run -d --name "$name" --label graphgo.scale=1 \
    -e WHOAMI_PORT_NUMBER=8080 --memory 32m "$IMAGE" >/dev/null
done
echo "$(docker ps -q --filter label=graphgo.scale=1 | wc -l) scale containers running."
