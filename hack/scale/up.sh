#!/usr/bin/env bash
# Spin up a kind cluster with KWOK fake nodes and a synthetic workload so
# graph-go can be exercised at high pod counts without running real
# containers. Pods are scheduled onto KWOK nodes, so they cost etcd/apiserver
# memory only (roughly 1.5GB for the control plane at 5k pods).
#
# Usage: hack/scale/up.sh [namespaces] [deployments-per-ns] [replicas]
#   defaults: 20 namespaces x 5 deployments x 5 replicas = 500 pods
#
# Re-running with bigger numbers scales the existing cluster up in place.
# Writes the kubeconfig to hack/scale/.kubeconfig so your current context is
# left alone. Point graph-go at it with hack/scale/scale.yaml.
set -euo pipefail

NAMESPACES=${1:-20}
DEPLOYS=${2:-5}
REPLICAS=${3:-5}

CLUSTER=${CLUSTER:-graphgo-scale}
KWOK_VERSION=${KWOK_VERSION:-v0.7.0}
FAKE_NODES=${FAKE_NODES:-10}
# Every Nth deployment is unschedulable so health isn't uniformly green.
UNHEALTHY_EVERY=${UNHEALTHY_EVERY:-7}

DIR=$(cd "$(dirname "$0")" && pwd)
export KUBECONFIG="$DIR/.kubeconfig"

if ! kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
  kind create cluster --name "$CLUSTER" --kubeconfig "$KUBECONFIG" --wait 120s
elif [[ ! -f $KUBECONFIG ]]; then
  # Reusing a cluster whose kubeconfig was deleted: write it again.
  kind export kubeconfig --name "$CLUSTER" --kubeconfig "$KUBECONFIG"
fi

if ! kubectl get deploy -n kube-system kwok-controller >/dev/null 2>&1; then
  kubectl apply -f "https://github.com/kubernetes-sigs/kwok/releases/download/${KWOK_VERSION}/kwok.yaml"
  kubectl apply -f "https://github.com/kubernetes-sigs/kwok/releases/download/${KWOK_VERSION}/stage-fast.yaml"
  kubectl -n kube-system rollout status deploy/kwok-controller --timeout=180s
fi

for i in $(seq 1 "$FAKE_NODES"); do
  cat <<EOF
---
apiVersion: v1
kind: Node
metadata:
  name: kwok-node-$i
  annotations:
    kwok.x-k8s.io/node: fake
  labels:
    type: kwok
    kubernetes.io/hostname: kwok-node-$i
spec:
  taints:
    - key: kwok.x-k8s.io/node
      value: fake
      effect: NoSchedule
status:
  allocatable: {cpu: "1000", memory: 1Ti, pods: "2000"}
  capacity: {cpu: "1000", memory: 1Ti, pods: "2000"}
EOF
done | kubectl apply -f - >/dev/null

gen() {
  local n d ns name fail selector
  for n in $(seq 1 "$NAMESPACES"); do
    # Fixed-width names so re-running with more namespaces scales the
    # existing ones instead of creating scale-001 next to scale-01.
    ns=$(printf 'scale-%02d' "$n")
    printf -- '---\napiVersion: v1\nkind: Namespace\nmetadata:\n  name: %s\n' "$ns"
    for d in $(seq 1 "$DEPLOYS"); do
      name="app-$d"
      fail=false
      selector='{type: kwok}'
      # Keyed on namespace/deployment numbers only, so re-running with a
      # different DEPLOYS never flips an existing deployment's health.
      if (( (n * 31 + d) % UNHEALTHY_EVERY == 0 )); then
        fail=true
        # No node carries this label, so these pods stay Pending forever.
        selector='{type: kwok, graphgo.scale/none: "true"}'
      fi
      cat <<EOF
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: $name, namespace: $ns, labels: {app: $name, tier: "t$((d % 3))"}}
spec:
  replicas: $REPLICAS
  selector: {matchLabels: {app: $name}}
  template:
    metadata: {labels: {app: $name, tier: "t$((d % 3))", graphgo.scale/fail: "$fail"}}
    spec:
      nodeSelector: $selector
      tolerations: [{key: kwok.x-k8s.io/node, operator: Exists, effect: NoSchedule}]
      containers: [{name: app, image: fake/$name:latest}]
---
apiVersion: v1
kind: Service
metadata: {name: $name, namespace: $ns}
spec:
  selector: {app: $name}
  ports: [{port: 80, targetPort: 8080}]
EOF
    done
  done
}

gen | kubectl apply -f - >/dev/null

echo "Waiting for pods to settle..."
want=$((10#$NAMESPACES * DEPLOYS * REPLICAS))
for _ in $(seq 1 120); do
  total=$(kubectl get pods -A -l graphgo.scale/fail --no-headers 2>/dev/null | wc -l)
  running=$(kubectl get pods -A -l graphgo.scale/fail=false --field-selector=status.phase=Running --no-headers 2>/dev/null | wc -l)
  echo "  pods: $total/$want (running: $running)"
  (( total >= want )) && break
  sleep 5
done
if (( total < want )); then
  echo "Timed out with $total/$want pods; not measuring against a partial cluster." >&2
  exit 1
fi

echo "Cluster $CLUSTER ready: $NAMESPACES ns x $DEPLOYS deploys x $REPLICAS replicas (~$want pods)."
echo "Run: graph-go serve -c $DIR/scale.yaml"
