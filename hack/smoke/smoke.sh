#!/usr/bin/env bash
# End-to-end smoke run: the real binary and image against real containers,
# the way users run them. Unit and integration tests can't catch "it works
# in the demo but the binary on my host sees nothing" (see the address
# resolution fix); this does. Run it before opening a PR: `make smoke`.
#
# What it checks, against two Compose projects (hack/smoke/compose.yml):
#   host      bin/graph-go scan from the host: every fixture service healthy,
#             Postgres tables discovered, colliding names qualified (#43),
#             node and edge IDs unique (#62)
#   mcp       graph-go mcp over stdio: handshake, the three tools, and a
#             tools/call that returns the fixture
#   image     the Docker image, run like the README: on the default bridge
#             (published ports via the gateway) and attached to a project
#             network (container IPs); graph-go must not list itself
#
# Env: SMOKE_IMAGE=0 skips the image build and runs (fastest loop).
#      SMOKE_KEEP=1 leaves the fixture running afterwards.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
cd "$ROOT"

COMPOSE_FILE=hack/smoke/compose.yml
PROJECTS=(ggsmoke-a ggsmoke-b)
BIN=bin/graph-go-smoke
IMAGE=graph-go:smoke
SELF_NAME=ggsmoke-self

failures=0
if [[ -t 1 ]]; then G=$'\033[32m' R=$'\033[31m' B=$'\033[1m' N=$'\033[0m'; else G='' R='' B='' N=''; fi
pass() { printf '  %s✓%s %s\n' "$G" "$N" "$1"; }
fail() { printf '  %s✗%s %s\n' "$R" "$N" "$1"; failures=$((failures + 1)); }
step() { printf '\n%s%s%s\n' "$B" "$1" "$N"; }

# coproc needs bash 4.0 and `exec {fd}>&-` 4.1; macOS ships 3.2 as /bin/bash.
if ((BASH_VERSINFO[0] < 4 || (BASH_VERSINFO[0] == 4 && BASH_VERSINFO[1] < 1))); then
	echo "smoke: bash 4.1+ is required (macOS: brew install bash)" >&2
	exit 2
fi

for tool in docker go jq; do
	command -v "$tool" >/dev/null || { echo "smoke: $tool is required" >&2; exit 2; }
done

cleanup() {
	docker rm -f "$SELF_NAME" >/dev/null 2>&1 || true
	if [[ "${SMOKE_KEEP:-0}" != 1 ]]; then
		for p in "${PROJECTS[@]}"; do
			docker compose -p "$p" -f "$COMPOSE_FILE" down -v --remove-orphans >/dev/null 2>&1 || true
		done
	fi
}
trap cleanup EXIT

step "fixture"
for p in "${PROJECTS[@]}"; do
	docker compose -p "$p" -f "$COMPOSE_FILE" up -d --wait --quiet-pull >/dev/null 2>&1 ||
		{ docker compose -p "$p" -f "$COMPOSE_FILE" logs --tail 20; echo "smoke: fixture $p failed to start" >&2; exit 2; }
done
pass "projects ${PROJECTS[*]} up and healthy"

step "build"
go build -o "$BIN" ./cmd/app
pass "$BIN ($("$BIN" version | head -1))"

# check_graph LABEL FILE EXPECT... : each EXPECT is a service name that must
# be a healthy service node. Postgres services must also expose the seeded
# `orders` table somewhere below them.
check_graph() {
	local label=$1 file=$2
	shift 2
	if ! jq -e '(.data // .).nodes' "$file" >/dev/null 2>&1; then
		fail "$label: scan output is not a graph"
		return
	fi
	for svc in "$@"; do
		local health
		health=$(jq -r --arg id "service-$svc" '(.data // .).nodes[] | select(.id == $id) | .health' "$file")
		if [[ "$health" == healthy ]]; then
			pass "$label: $svc healthy"
		else
			fail "$label: $svc ${health:-missing}"
		fi
		if [[ "$svc" == *.pg ]]; then
			if jq -e --arg id "service-$svc" '
				(.data // .) as $g
				| [$g.nodes[] | select(.parent == $id) | .id] as $dbs
				| any($g.nodes[]; .type == "table" and .name == "orders" and (.parent as $p | $dbs | index($p)))
			' "$file" >/dev/null; then
				pass "$label: $svc tables discovered"
			else
				fail "$label: $svc has no orders table"
			fi
		fi
	done
}

# check_unique_ids LABEL FILE: no node or edge id may appear twice, and every
# parent and edge endpoint must name an existing node (#62).
check_unique_ids() {
	local label=$1 file=$2 dups dangling
	dups=$(jq -r '(.data // .) as $g | ([$g.nodes[].id], [$g.edges[].id]) | group_by(.) | map(select(length > 1) | .[0]) | .[]' "$file" | sort -u | head -5 | tr '\n' ' ')
	if [[ -z "$dups" ]]; then pass "$label: node and edge ids unique"; else fail "$label: duplicate ids: $dups"; fi
	dangling=$(jq -r '
		(.data // .) as $g | ([$g.nodes[].id] | map({(.): true}) | add // {}) as $ids
		| ([$g.nodes[] | .parent // empty] + [$g.edges[] | .source, .target])
		| map(select($ids[.] | not)) | unique | .[]
	' "$file" | head -5 | tr '\n' ' ')
	if [[ -z "$dangling" ]]; then pass "$label: parents and edges resolve"; else fail "$label: dangling refs: $dangling"; fi
}

step "host: graph-go scan"
"$BIN" scan --format json --log-level=error >/tmp/ggsmoke-host.json || true
# pg is deliberately unpublished. A Linux host reaches it by container IP;
# Docker Desktop (macOS, Windows) can't route to container IPs, so there only
# the published caches are reachable, as the README documents.
if [[ "$(uname -s)" == Linux ]]; then
	check_graph host /tmp/ggsmoke-host.json ggsmoke-a.pg ggsmoke-b.pg ggsmoke-a.cache ggsmoke-b.cache
else
	check_graph host /tmp/ggsmoke-host.json ggsmoke-a.cache ggsmoke-b.cache
fi
check_unique_ids host /tmp/ggsmoke-host.json

step "host: graph-go mcp"
coproc MCP { "$BIN" mcp --log-level=error; }
mcp_send() { printf '%s\n' "$1" >&"${MCP[1]}"; }
# mcp_recv ID: prints the response with that id (60s budget). Call it as
# $(mcp_recv ID), never in a pipeline: the coproc's descriptors aren't
# available in a pipeline's subshell.
mcp_recv() {
	local line
	while IFS= read -r -t 60 line <&"${MCP[0]}"; do
		if jq -e --argjson id "$1" '.id == $id' <<<"$line" >/dev/null 2>&1; then
			printf '%s\n' "$line"
			return 0
		fi
	done
	return 1
}
mcp_send '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"smoke","version":"0"}}}'
hello=$(mcp_recv 1 || true)
if jq -e '.result.serverInfo.name == "graph-go"' <<<"$hello" >/dev/null 2>&1; then pass "mcp: handshake"; else fail "mcp: handshake"; fi
mcp_send '{"jsonrpc":"2.0","method":"notifications/initialized"}'
mcp_send '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
listed=$(mcp_recv 2 || true)
tools=$(jq -r '[.result.tools[].name] | sort | join(",")' <<<"$listed" 2>/dev/null || true)
if [[ "$tools" == "find_nodes,get_node,get_topology" ]]; then pass "mcp: tools $tools"; else fail "mcp: tools '${tools}'"; fi
mcp_send '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"find_nodes","arguments":{"query":"ggsmoke-a.pg"}}}'
found=$(mcp_recv 3 || true)
if jq -e '(.result.isError | not) and (.result | tostring | contains("service-ggsmoke-a.pg"))' <<<"$found" >/dev/null 2>&1; then
	pass "mcp: find_nodes returns the fixture"
else
	fail "mcp: find_nodes did not return service-ggsmoke-a.pg"
fi
exec {MCP[1]}>&-
wait "$MCP_PID" 2>/dev/null || true

if [[ "${SMOKE_IMAGE:-1}" == 0 ]]; then
	step "image: skipped (SMOKE_IMAGE=0)"
else
	step "image: docker build"
	if ! docker build -q -t "$IMAGE" . >/tmp/ggsmoke-build.log 2>&1; then
		tail -30 /tmp/ggsmoke-build.log
		echo "smoke: image build failed" >&2
		exit 2
	fi
	pass "$IMAGE built"

	gid=$(stat -c %g /var/run/docker.sock 2>/dev/null || stat -f %g /var/run/docker.sock)
	run_image() { # run_image OUTFILE [docker run flags...]
		local out=$1
		shift
		docker run --rm --name "$SELF_NAME" "$@" \
			-v /var/run/docker.sock:/var/run/docker.sock:ro --group-add "$gid" \
			"$IMAGE" ./graph-go scan --format json --log-level=error >"$out" || true
	}

	step "image: default bridge (README docker run)"
	run_image /tmp/ggsmoke-bridge.json
	# pg isn't published, so from the default bridge only the caches are reachable.
	check_graph bridge /tmp/ggsmoke-bridge.json ggsmoke-a.cache ggsmoke-b.cache
	if jq -e --arg n "$SELF_NAME" 'any((.data // .).nodes[]; .name == $n)' /tmp/ggsmoke-bridge.json >/dev/null; then
		fail "bridge: graph-go lists its own container"
	else
		pass "bridge: graph-go doesn't list itself"
	fi

	step "image: attached to ggsmoke-a_default"
	run_image /tmp/ggsmoke-network.json --network ggsmoke-a_default
	check_graph network /tmp/ggsmoke-network.json ggsmoke-a.pg ggsmoke-a.cache ggsmoke-b.cache
fi

step "result"
if ((failures > 0)); then
	echo "smoke: $failures check(s) failed"
	exit 1
fi
echo "smoke: all checks passed"
