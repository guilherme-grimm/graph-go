import type { Graph, GraphNode } from './graph';

export interface ApiResponse<T> {
  data: T;
  error?: string;
}

export interface GraphResponse extends ApiResponse<Graph> {}

export interface NodeResponse extends ApiResponse<GraphNode> {}

export interface HealthResponse {
  status: 'ok' | 'degraded' | 'error';
  timestamp: string;
  version?: string;
}

export interface WebSocketMessage {
  type: 'health_update' | 'node_update' | 'graph_update';
  payload: unknown;
}

export interface HealthUpdatePayload {
  nodeId: string;
  health: GraphNode['health'];
}

export interface NodeUpdatePayload {
  node: GraphNode;
}
