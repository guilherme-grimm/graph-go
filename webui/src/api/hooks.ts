import { useCallback } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import type { Graph, HealthUpdatePayload } from '../types';

// Query keys for cache management
export const queryKeys = {
  graph: ['graph'] as const,
  node: (id: string) => ['node', id] as const,
  health: ['health'] as const,
};

export function useGraph() {
  return useQuery({
    queryKey: queryKeys.graph,
    queryFn: async () => {
      const response = await api.getGraph();
      return response.data;
    },
    staleTime: 30_000,
    retry: 3,
    refetchOnWindowFocus: false,
  });
}

export function useNode(id: string | null) {
  return useQuery({
    queryKey: queryKeys.node(id ?? ''),
    queryFn: async () => {
      if (!id) return null;
      const response = await api.getNode(id);
      return response.data;
    },
    enabled: !!id,
    staleTime: 30_000,
  });
}

export function useHealth() {
  return useQuery({
    queryKey: queryKeys.health,
    queryFn: api.getHealth,
    refetchInterval: 30_000, // Poll every 30s
    staleTime: 10_000,
  });
}

// Hook to apply a batch of node health changes to the cached graph (used by
// WebSocket). One cache write per batch, so a tick touching thousands of
// nodes costs one re-render instead of thousands.
export function useApplyHealthBatch() {
  const queryClient = useQueryClient();

  return useCallback(
    (updates: HealthUpdatePayload[]) => {
      if (updates.length === 0) return;
      queryClient.setQueryData<Graph>(queryKeys.graph, (oldData) => {
        if (!oldData?.nodes) return oldData;

        const next = new Map(updates.map(u => [u.nodeId, u.health]));
        let changed = false;
        const nodes = oldData.nodes.map((node) => {
          const health = next.get(node.id);
          if (health === undefined || health === node.health) return node;
          changed = true;
          return { ...node, health };
        });
        return changed ? { ...oldData, nodes } : oldData;
      });
    },
    [queryClient]
  );
}

// Hook to get a specific node from cached graph data
export function useNodeFromGraph(nodeId: string | null) {
  const { data: graph } = useGraph();

  if (!nodeId || !graph?.nodes) return null;

  return graph.nodes.find((node) => node.id === nodeId) ?? null;
}
