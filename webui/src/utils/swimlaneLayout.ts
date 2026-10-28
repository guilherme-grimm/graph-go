import type { Graph, GraphNode, NodeType } from '../types';

// ── Constants ───────────────────────────────────────────────────────────
const NODE_W = 200;
const NODE_H_SERVICE = 80;
const NODE_H_WORKLOAD = 80;
const NODE_H_POD = 72;
const LANE_GAP_Y = 32;
const LANE_PAD_X = 24;
const LANE_PAD_Y = 16;
const LANE_HEADER_H = 40;
const COLUMN_GAP = 48;
const ROW_GAP = 16;
const POD_GAP_X = 16;
// Pods sit on their workload's row and wrap after this many, so a lane grows
// sideways a little instead of one column of pods growing without bound.
const PODS_PER_ROW = 6;
// Collapsed lanes are header-only; consecutive ones pack into a grid so a
// fully collapsed cluster reads as an overview instead of a tall strip.
const COLLAPSED_COLS = 4;
const COLLAPSED_W = 420;
const COLLAPSED_GAP = 12;

// ── Zone classification for lane ordering ───────────────────────────────
const SYSTEM_NAMESPACES = new Set([
  'kube-system',
  'kube-public',
  'kube-node-lease',
]);

const INFRA_NAMESPACES = new Set([
  'ingress-nginx',
  'cert-manager',
  'local-path-storage',
  'metrics-server',
  'monitoring',
  'observability',
  'metallb-system',
  'kube-flannel',
  'calico-system',
  'tigera-operator',
]);

const K8S_SERVICE_TYPES: ReadonlySet<NodeType> = new Set<NodeType>([
  'k8s_service',
]);

const K8S_WORKLOAD_TYPES: ReadonlySet<NodeType> = new Set<NodeType>([
  'deployment',
  'statefulset',
  'daemonset',
]);

const K8S_POD_TYPES: ReadonlySet<NodeType> = new Set<NodeType>([
  'pod',
]);

type Zone = 'system' | 'user' | 'infra';

function classifyNamespace(name: string): Zone {
  if (SYSTEM_NAMESPACES.has(name)) return 'system';
  if (INFRA_NAMESPACES.has(name)) return 'infra';
  return 'user';
}

const ZONE_ORDER: Record<Zone, number> = { system: 0, user: 1, infra: 2 };

// Natural order, so app-2 sorts before app-10.
const nameCollator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });
const byName = (a: string, b: string) => nameCollator.compare(a, b);

// ── Lane data structures ────────────────────────────────────────────────
interface Lane {
  namespaceId: string;
  namespaceName: string;
  zone: Zone;
  services: GraphNode[];
  workloads: GraphNode[];
  pods: GraphNode[];
}

// ── Exported types for consumers ────────────────────────────────────────
export interface SwimlanePositions {
  positions: Map<string, { x: number; y: number }>;
  laneBounds: Map<string, { x: number; y: number; w: number; h: number }>;
}

/**
 * Calculate deterministic swimlane layout for k8s graphs.
 *
 * Each namespace becomes a horizontal lane.
 * Lanes are sorted: system namespaces top, user namespaces middle (alpha),
 * infra namespaces bottom.
 *
 * Within each lane: services stack in the left column; each workload gets a
 * row with its pods laid out to its right (wrapping after PODS_PER_ROW).
 * Pods without an owning workload fill rows at the end.
 *
 * Lanes whose namespace is collapsed render header-only and pack into a grid.
 */
export function calculateSwimlaneLayout(
  graph: Graph,
  isCollapsed: (namespaceId: string) => boolean = () => false,
): SwimlanePositions {
  const positions = new Map<string, { x: number; y: number }>();
  const laneBounds = new Map<string, { x: number; y: number; w: number; h: number }>();

  if (!graph.nodes.length) return { positions, laneBounds };

  // ── Build lanes ───────────────────────────────────────────────────────
  const namespaceNodes = new Map<string, GraphNode>();
  const childrenByNs = new Map<string, GraphNode[]>();

  for (const node of graph.nodes) {
    if (node.type === 'namespace') {
      namespaceNodes.set(node.id, node);
      if (!childrenByNs.has(node.id)) childrenByNs.set(node.id, []);
    }
  }

  // Bucket children into their parent namespace.
  for (const node of graph.nodes) {
    if (node.type === 'namespace') continue;
    const parentId = node.parent;
    if (parentId && childrenByNs.has(parentId)) {
      childrenByNs.get(parentId)!.push(node);
    } else if (parentId) {
      // Parent namespace not in graph — create a placeholder lane.
      if (!childrenByNs.has(parentId)) childrenByNs.set(parentId, []);
      childrenByNs.get(parentId)!.push(node);
    }
    // Non-k8s nodes without parent are skipped (they shouldn't be in k8s view)
  }

  // Build ownership index: workload → pod names (for pod clustering).
  // We derive ownership from "contains" edges: workload -contains-> pod.
  const nodeById = new Map(graph.nodes.map(n => [n.id, n]));
  const podOwner = new Map<string, string>();
  for (const edge of graph.edges) {
    if (edge.type === 'contains') {
      // source = workload, target = pod (or source = namespace, target = workload)
      const targetNode = nodeById.get(edge.target);
      if (targetNode && targetNode.type === 'pod') {
        podOwner.set(edge.target, edge.source);
      }
    }
  }

  // Each service sits on the row of the workload it routes the most pods
  // to, so its edge is short and horizontal instead of crossing the lane.
  const routedCounts = new Map<string, Map<string, number>>();
  for (const edge of graph.edges) {
    if (edge.type !== 'routes_to') continue;
    const owner = podOwner.get(edge.target);
    if (!owner) continue;
    if (!routedCounts.has(edge.source)) routedCounts.set(edge.source, new Map());
    const counts = routedCounts.get(edge.source)!;
    counts.set(owner, (counts.get(owner) ?? 0) + 1);
  }
  const serviceWorkload = new Map<string, string>();
  for (const [svc, counts] of routedCounts) {
    let best: string | undefined;
    for (const [w, n] of counts) if (best === undefined || n > counts.get(best)!) best = w;
    if (best) serviceWorkload.set(svc, best);
  }

  // Build sorted lanes.
  const lanes: Lane[] = [];
  for (const [nsId, children] of childrenByNs) {
    const nsNode = namespaceNodes.get(nsId);
    const nsName = nsNode?.name ?? nsId.replace(/^k8s-namespace-/, '');

    const services: GraphNode[] = [];
    const workloads: GraphNode[] = [];
    const pods: GraphNode[] = [];

    for (const child of children) {
      if (K8S_SERVICE_TYPES.has(child.type)) {
        services.push(child);
      } else if (K8S_WORKLOAD_TYPES.has(child.type)) {
        workloads.push(child);
      } else if (K8S_POD_TYPES.has(child.type)) {
        pods.push(child);
      }
      // Other types fall through — placed at end of workloads column.
    }

    // Sort workloads alphabetically.
    workloads.sort((a, b) => byName(a.name, b.name));
    services.sort((a, b) => byName(a.name, b.name));

    lanes.push({
      namespaceId: nsId,
      namespaceName: nsName,
      zone: classifyNamespace(nsName),
      services,
      workloads,
      pods,
    });
  }

  // Sort lanes: system → user (alpha) → infra (alpha).
  lanes.sort((a, b) => {
    const zoneA = ZONE_ORDER[a.zone];
    const zoneB = ZONE_ORDER[b.zone];
    if (zoneA !== zoneB) return zoneA - zoneB;
    return byName(a.namespaceName, b.namespaceName);
  });

  // ── Position nodes in lanes ───────────────────────────────────────────
  let currentY = 0;
  let gridCol = 0; // next free column in the current collapsed-lane grid row

  for (const lane of lanes) {
    const nsNode = namespaceNodes.get(lane.namespaceId);
    const hasContent = lane.services.length + lane.workloads.length + lane.pods.length > 0;

    if (!hasContent && isCollapsed(lane.namespaceId)) {
      const x = gridCol * (COLLAPSED_W + COLLAPSED_GAP);
      if (nsNode) positions.set(nsNode.id, { x, y: currentY });
      laneBounds.set(lane.namespaceId, { x, y: currentY, w: COLLAPSED_W, h: LANE_HEADER_H });
      gridCol++;
      if (gridCol === COLLAPSED_COLS) {
        gridCol = 0;
        currentY += LANE_HEADER_H + COLLAPSED_GAP;
      }
      continue;
    }
    if (gridCol > 0) {
      gridCol = 0;
      currentY += LANE_HEADER_H + LANE_GAP_Y;
    }

    const laneStartY = currentY;
    if (nsNode) positions.set(nsNode.id, { x: 0, y: laneStartY });

    const contentStartY = laneStartY + LANE_HEADER_H + LANE_PAD_Y;

    // Group pods under their owning workload; the rest are orphans.
    const podsByOwner = new Map<string, GraphNode[]>();
    const orphanPods: GraphNode[] = [];
    const workloadIds = new Set(lane.workloads.map(w => w.id));
    for (const pod of lane.pods) {
      const owner = podOwner.get(pod.id);
      if (owner && workloadIds.has(owner)) {
        if (!podsByOwner.has(owner)) podsByOwner.set(owner, []);
        podsByOwner.get(owner)!.push(pod);
      } else {
        orphanPods.push(pod);
      }
    }
    for (const pods of podsByOwner.values()) pods.sort((a, b) => byName(a.name, b.name));
    orphanPods.sort((a, b) => byName(a.name, b.name));

    // Column x offsets. Empty columns collapse.
    let colX = LANE_PAD_X;
    const serviceColX = colX;
    if (lane.services.length > 0) colX += NODE_W + COLUMN_GAP;
    const workloadColX = colX;
    if (lane.workloads.length > 0) colX += NODE_W + COLUMN_GAP;
    const podColX = colX;

    // Services grouped by the workload they front; the rest stack after
    // the workload rows.
    const servicesByWorkload = new Map<string, GraphNode[]>();
    const looseServices: GraphNode[] = [];
    for (const svc of lane.services) {
      const w = serviceWorkload.get(svc.id);
      if (w && workloadIds.has(w)) {
        if (!servicesByWorkload.has(w)) servicesByWorkload.set(w, []);
        servicesByWorkload.get(w)!.push(svc);
      } else {
        looseServices.push(svc);
      }
    }
    const placeServices = (svcs: GraphNode[], y: number): number => {
      svcs.forEach((svc, i) => positions.set(svc.id, { x: serviceColX, y: y + i * (NODE_H_SERVICE + ROW_GAP) }));
      return svcs.length === 0 ? 0 : svcs.length * NODE_H_SERVICE + (svcs.length - 1) * ROW_GAP;
    };

    // Lay out a run of pods as a grid starting at (podColX, y); returns the
    // height used.
    let widestPodRow = 0;
    const placePods = (pods: GraphNode[], y: number): number => {
      for (let i = 0; i < pods.length; i++) {
        const col = i % PODS_PER_ROW;
        const row = Math.floor(i / PODS_PER_ROW);
        positions.set(pods[i].id, { x: podColX + col * (NODE_W + POD_GAP_X), y: y + row * (NODE_H_POD + ROW_GAP) });
      }
      widestPodRow = Math.max(widestPodRow, Math.min(pods.length, PODS_PER_ROW));
      const rows = Math.ceil(pods.length / PODS_PER_ROW);
      return rows === 0 ? 0 : rows * NODE_H_POD + (rows - 1) * ROW_GAP;
    };

    // One row per workload: its services, itself, then its wrapped pods.
    let rowY = contentStartY;
    for (const w of lane.workloads) {
      positions.set(w.id, { x: workloadColX, y: rowY });
      const podsH = placePods(podsByOwner.get(w.id) ?? [], rowY);
      const svcH = placeServices(servicesByWorkload.get(w.id) ?? [], rowY);
      rowY += Math.max(NODE_H_WORKLOAD, podsH, svcH) + ROW_GAP;
    }
    if (orphanPods.length > 0 || looseServices.length > 0) {
      const podsH = placePods(orphanPods, rowY);
      const svcH = placeServices(looseServices, rowY);
      rowY += Math.max(podsH, svcH) + ROW_GAP;
    }
    const contentMaxY = rowY > contentStartY ? rowY - ROW_GAP : contentStartY;
    const laneH = hasContent
      ? (contentMaxY - laneStartY) + LANE_PAD_Y
      : LANE_HEADER_H + LANE_PAD_Y;

    let laneW: number;
    if (widestPodRow > 0) {
      laneW = podColX + widestPodRow * NODE_W + (widestPodRow - 1) * POD_GAP_X + LANE_PAD_X;
    } else if (lane.workloads.length > 0) {
      laneW = workloadColX + NODE_W + LANE_PAD_X;
    } else if (lane.services.length > 0) {
      laneW = serviceColX + NODE_W + LANE_PAD_X;
    } else {
      laneW = LANE_PAD_X * 2 + NODE_W; // minimum width for empty lanes
    }
    // Never narrower than a collapsed lane, so headers have room for stats.
    laneW = Math.max(laneW, COLLAPSED_W);

    laneBounds.set(lane.namespaceId, { x: 0, y: laneStartY, w: laneW, h: laneH });
    currentY = laneStartY + laneH + LANE_GAP_Y;
  }

  return { positions, laneBounds };
}

/**
 * Returns true if the graph contains namespace nodes, indicating it's a k8s
 * graph that should default to swimlane layout.
 */
export function hasNamespaces(graph: Graph): boolean {
  return graph.nodes.some(n => n.type === 'namespace');
}
