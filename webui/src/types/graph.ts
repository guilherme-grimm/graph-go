export type HealthStatus = 'healthy' | 'degraded' | 'unhealthy' | 'unknown';

export type NodeType =
  | 'database'
  | 'bucket'
  | 'service'
  | 'table'
  | 'collection'
  | 'api'
  | 'queue'
  | 'cache'
  | 'storage'
  | 'gateway'
  | 'payment'
  | 'auth';

// Priority tiers for visual hierarchy
export type PriorityTier = 'critical' | 'high' | 'medium' | 'low';

export interface NodeMetadata {
  tables?: string[];
  collections?: string[];
  endpoints?: string[];
  size?: string;
  region?: string;
  version?: string;
  priority?: PriorityTier;
  [key: string]: unknown;
}

export interface GraphNode {
  id: string;
  type: NodeType;
  name: string;
  parent?: string;
  metadata: NodeMetadata;
  health: HealthStatus;
}

export interface GraphEdge {
  id: string;
  source: string;
  target: string;
  type: string;
  label?: string;
}

export interface Graph {
  nodes: GraphNode[];
  edges: GraphEdge[];
}
