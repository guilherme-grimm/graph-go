#!/usr/bin/env bash
# Tear down the scale-test cluster created by up.sh.
set -euo pipefail
DIR=$(cd "$(dirname "$0")" && pwd)
kind delete cluster --name "${CLUSTER:-graphgo-scale}" --kubeconfig "$DIR/.kubeconfig"
rm -f "$DIR/.kubeconfig"
