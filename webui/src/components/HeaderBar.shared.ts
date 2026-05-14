import type { HealthStatus, NodeType } from '../types';

export type NodeSource = 'docker' | 'kubernetes';

export interface Filters {
  types: NodeType[];
  health: HealthStatus[];
  sources: NodeSource[];
}

export type LayoutMode = 'hierarchical' | 'force' | 'swimlane';

export const K8S_TYPES: ReadonlySet<NodeType> = new Set<NodeType>([
  'namespace', 'deployment', 'statefulset', 'daemonset', 'pod', 'k8s_service',
]);

export function nodeSource(type: NodeType): NodeSource {
  return K8S_TYPES.has(type) ? 'kubernetes' : 'docker';
}
