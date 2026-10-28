import type { Graph, GraphEdge, GraphNode, HealthStatus, NodeType } from '../types';

// Above this many nodes a graph is "large": namespaces start collapsed and
// layouts other than swimlane are swapped out (they turn into a haystack and
// cost seconds of main-thread time to compute).
export const LARGE_GRAPH_NODES = 300;

export type NodeSource = 'docker' | 'kubernetes';

// K8s node types — used to infer source from node type.
export const K8S_TYPES: ReadonlySet<NodeType> = new Set<NodeType>([
  'namespace', 'deployment', 'statefulset', 'daemonset', 'pod', 'k8s_service',
]);

export function nodeSource(type: NodeType): NodeSource {
  return K8S_TYPES.has(type) ? 'kubernetes' : 'docker';
}

export interface Filters {
  types: NodeType[];
  health: HealthStatus[];
  sources: NodeSource[];
  // Namespace names whose lanes (and everything inside them) are hidden.
  hiddenNamespaces: string[];
  // Case-insensitive substring matched against node names.
  query: string;
}

export const EMPTY_FILTERS: Filters = { types: [], health: [], sources: [], hiddenNamespaces: [], query: '' };

// Each facet is one predicate; a node is shown when every active facet keeps
// it. Adding a facet (e.g. labels) means adding a field to Filters and one
// entry here — the URL codec below and the view pipeline pick it up.
type Facet = (filters: Filters) => ((node: GraphNode) => boolean) | null;

const FACETS: Facet[] = [
  f => (f.types.length ? n => f.types.includes(n.type) : null),
  f => (f.health.length ? n => f.health.includes(n.health) : null),
  f => (f.sources.length ? n => f.sources.includes(nodeSource(n.type)) : null),
  f => {
    if (!f.query.trim()) return null;
    const q = f.query.trim().toLowerCase();
    return n => n.name.toLowerCase().includes(q);
  },
];

export function activeFilterCount(f: Filters): number {
  return f.types.length + f.health.length + f.sources.length + f.hiddenNamespaces.length + (f.query.trim() ? 1 : 0);
}

export function filtersFromParams(params: URLSearchParams): Filters {
  const list = (key: string) => (params.get(key) ?? '').split(',').filter(Boolean);
  return {
    types: list('types') as NodeType[],
    health: list('health') as HealthStatus[],
    sources: list('sources') as NodeSource[],
    hiddenNamespaces: list('hide'),
    query: params.get('q') ?? '',
  };
}

export function writeFiltersToParams(f: Filters, params: URLSearchParams): void {
  const set = (key: string, value: string) => (value ? params.set(key, value) : params.delete(key));
  set('types', f.types.join(','));
  set('health', f.health.join(','));
  set('sources', f.sources.join(','));
  set('hide', f.hiddenNamespaces.join(','));
  set('q', f.query.trim());
}

// Namespace a node lives in: its own id for namespace nodes, otherwise the
// parent when that parent is a namespace.
export function namespaceOf(node: GraphNode, nodeById: Map<string, GraphNode>): string | undefined {
  if (node.type === 'namespace') return node.id;
  if (!node.parent) return undefined;
  return nodeById.get(node.parent)?.type === 'namespace' ? node.parent : undefined;
}

export function applyFilters(graph: Graph, f: Filters): Graph {
  const predicates = FACETS.map(facet => facet(f)).filter((p): p is (n: GraphNode) => boolean => p !== null);
  if (predicates.length === 0 && f.hiddenNamespaces.length === 0) return graph;

  const nodeById = new Map(graph.nodes.map(n => [n.id, n]));
  const hidden = new Set(f.hiddenNamespaces);
  const isHidden = (n: GraphNode) => {
    const ns = namespaceOf(n, nodeById);
    return ns !== undefined && hidden.has(nodeById.get(ns)!.name);
  };

  const kept = new Map<string, GraphNode>();
  for (const node of graph.nodes) {
    if (isHidden(node)) continue;
    if (predicates.every(p => p(node))) kept.set(node.id, node);
  }
  // Keep the namespace of every surviving node so it still has a lane/group
  // to sit in, even when the namespace itself didn't match the filter.
  for (const node of [...kept.values()]) {
    const ns = namespaceOf(node, nodeById);
    if (ns && !kept.has(ns)) kept.set(ns, nodeById.get(ns)!);
  }

  const nodes = graph.nodes.filter(n => kept.has(n.id));
  const edges = graph.edges.filter(e => kept.has(e.source) && kept.has(e.target));
  return { nodes, edges };
}

// ── Namespace collapsing ────────────────────────────────────────────────

// Collapse state is "everything except" or "nothing except", so a large
// cluster can start fully collapsed without listing every namespace.
export interface CollapseState {
  all: boolean;
  except: string[]; // namespace names
}

export const NO_COLLAPSE: CollapseState = { all: false, except: [] };
export const ALL_COLLAPSED: CollapseState = { all: true, except: [] };

export function isCollapsed(state: CollapseState, nsName: string): boolean {
  return state.all !== state.except.includes(nsName);
}

export function toggleCollapsed(state: CollapseState, nsName: string): CollapseState {
  const except = state.except.includes(nsName)
    ? state.except.filter(n => n !== nsName)
    : [...state.except, nsName];
  return { ...state, except };
}

// URL form: collapse=all&expand=a,b | collapse=a,b | collapse=none.
// Returns null when the URL says nothing, so the caller can pick a default.
export function collapseFromParams(params: URLSearchParams): CollapseState | null {
  const collapse = params.get('collapse');
  if (collapse === null) return null;
  if (collapse === 'all') {
    return { all: true, except: (params.get('expand') ?? '').split(',').filter(Boolean) };
  }
  if (collapse === 'none') return NO_COLLAPSE;
  return { all: false, except: collapse.split(',').filter(Boolean) };
}

export function writeCollapseToParams(state: CollapseState, params: URLSearchParams): void {
  params.delete('expand');
  if (state.all) {
    params.set('collapse', 'all');
    if (state.except.length) params.set('expand', state.except.join(','));
  } else {
    params.set('collapse', state.except.length ? state.except.join(',') : 'none');
  }
}

export interface NamespaceSummary {
  collapsed: boolean;
  podCount: number;
  workloadCount: number;
  serviceCount: number;
  // Child health tally, used for the lane header and the collapsed dot.
  healthCounts: Record<HealthStatus, number>;
}

const WORKLOAD_TYPES: ReadonlySet<NodeType> = new Set<NodeType>(['deployment', 'statefulset', 'daemonset']);
const HEALTH_RANK: Record<HealthStatus, number> = { unhealthy: 3, degraded: 2, unknown: 1, healthy: 0 };

export function worstHealth(counts: Record<HealthStatus, number>, fallback: HealthStatus): HealthStatus {
  let worst: HealthStatus | null = null;
  for (const h of Object.keys(counts) as HealthStatus[]) {
    if (counts[h] > 0 && (worst === null || HEALTH_RANK[h] > HEALTH_RANK[worst])) worst = h;
  }
  return worst ?? fallback;
}

export interface GraphView {
  graph: Graph;
  // Per-namespace-id summary, computed from the graph before collapsing.
  namespaces: Map<string, NamespaceSummary>;
}

// Collapses namespaces: their children disappear, and edges touching a child
// are re-pointed at the namespace (deduplicated, self-loops dropped), so
// cross-namespace relationships stay visible on the collapsed node.
export function collapseNamespaces(graph: Graph, state: CollapseState): GraphView {
  const nodeById = new Map(graph.nodes.map(n => [n.id, n]));
  const namespaces = new Map<string, NamespaceSummary>();
  for (const n of graph.nodes) {
    if (n.type !== 'namespace') continue;
    namespaces.set(n.id, {
      collapsed: isCollapsed(state, n.name),
      podCount: 0,
      workloadCount: 0,
      serviceCount: 0,
      healthCounts: { healthy: 0, degraded: 0, unhealthy: 0, unknown: 0 },
    });
  }

  const owner = new Map<string, string>(); // child id -> collapsed namespace id
  for (const n of graph.nodes) {
    const ns = n.type === 'namespace' ? undefined : namespaceOf(n, nodeById);
    if (!ns) continue;
    const s = namespaces.get(ns)!;
    if (n.type === 'pod') s.podCount++;
    else if (WORKLOAD_TYPES.has(n.type)) s.workloadCount++;
    else if (n.type === 'k8s_service') s.serviceCount++;
    s.healthCounts[n.health] = (s.healthCounts[n.health] ?? 0) + 1;
    if (s.collapsed) owner.set(n.id, ns);
  }
  if (owner.size === 0) return { graph, namespaces };

  const nodes = graph.nodes.filter(n => !owner.has(n.id));
  const edges: GraphEdge[] = [];
  const seen = new Set<string>();
  for (const e of graph.edges) {
    const source = owner.get(e.source) ?? e.source;
    const target = owner.get(e.target) ?? e.target;
    if (source === e.source && target === e.target) {
      edges.push(e);
      continue;
    }
    if (source === target) continue;
    const id = `collapsed:${source}->${target}:${e.type}`;
    if (seen.has(id)) continue;
    seen.add(id);
    edges.push({ id, source, target, type: e.type, label: e.label });
  }
  return { graph: { nodes, edges }, namespaces };
}

export function namespaceNames(graph: Graph | undefined): string[] {
  if (!graph) return [];
  return graph.nodes.filter(n => n.type === 'namespace').map(n => n.name).sort();
}
