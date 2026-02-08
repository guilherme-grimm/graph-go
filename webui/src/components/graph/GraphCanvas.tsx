import { useCallback, useMemo, useEffect, useRef } from 'react';
import {
  ReactFlow,
  Background,
  Controls,
  useNodesState,
  useEdgesState,
  useReactFlow,
  ReactFlowProvider,
  type Node,
  type Edge,
  type NodeTypes,
  type EdgeTypes,
  BackgroundVariant,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';

import CustomNode, { type CustomNodeData } from './CustomNode';
import CustomEdge, { type CustomEdgeData } from './CustomEdge';
import { EmptyState } from '../ui';
import type { Graph, GraphNode, GraphEdge } from '../../types';
import { calculatePriority, countConnections } from '../../utils';
import styles from './GraphCanvas.module.css';

const nodeTypes: NodeTypes = {
  custom: CustomNode,
};

const edgeTypes: EdgeTypes = {
  custom: CustomEdge,
};

interface GraphCanvasProps {
  graph: Graph | undefined;
  selectedNodeId: string | null;
  onNodeSelect: (nodeId: string | null) => void;
  onEdgeClick?: (edge: GraphEdge) => void;
  isLoading?: boolean;
  error?: Error | null;
}

function getConnectedNodeIds(nodeId: string, edges: GraphEdge[]) {
  const sources = new Set<string>();
  const targets = new Set<string>();

  edges.forEach(edge => {
    if (edge.source === nodeId) targets.add(edge.target);
    if (edge.target === nodeId) sources.add(edge.source);
  });

  const all = new Set([...sources, ...targets]);
  return { sources, targets, all };
}

// Hierarchical layout: assign ranks via longest-path, then space nodes within each rank.
function calculateHierarchicalLayout(graph: Graph): Map<string, { x: number; y: number }> {
  const NODE_W = 180;
  const NODE_H = 64;
  const GAP_X = 40;
  const GAP_Y = 80;

  const nodeIds = new Set(graph.nodes.map(n => n.id));
  const children = new Map<string, string[]>();
  const parents = new Map<string, string[]>();

  for (const id of nodeIds) {
    children.set(id, []);
    parents.set(id, []);
  }

  for (const edge of graph.edges) {
    if (nodeIds.has(edge.source) && nodeIds.has(edge.target)) {
      children.get(edge.source)!.push(edge.target);
      parents.get(edge.target)!.push(edge.source);
    }
  }

  // Assign ranks using longest-path from roots (nodes with no incoming edges)
  const rank = new Map<string, number>();

  function assignRank(id: string): number {
    if (rank.has(id)) return rank.get(id)!;
    const pars = parents.get(id)!;
    const r = pars.length === 0 ? 0 : Math.max(...pars.map(assignRank)) + 1;
    rank.set(id, r);
    return r;
  }

  for (const id of nodeIds) assignRank(id);

  // Group nodes by rank
  const ranks = new Map<number, GraphNode[]>();
  for (const node of graph.nodes) {
    const r = rank.get(node.id) ?? 0;
    if (!ranks.has(r)) ranks.set(r, []);
    ranks.get(r)!.push(node);
  }

  // Position: center each rank row horizontally
  const positions = new Map<string, { x: number; y: number }>();
  const sortedRanks = [...ranks.keys()].sort((a, b) => a - b);

  for (const r of sortedRanks) {
    const nodesInRank = ranks.get(r)!;
    const totalWidth = nodesInRank.length * NODE_W + (nodesInRank.length - 1) * GAP_X;
    const startX = -totalWidth / 2;

    nodesInRank.forEach((node, i) => {
      positions.set(node.id, {
        x: startX + i * (NODE_W + GAP_X),
        y: r * (NODE_H + GAP_Y),
      });
    });
  }

  return positions;
}

function transformGraphToFlow(
  graph: Graph,
  selectedNodeId: string | null
): {
  nodes: Node<CustomNodeData>[];
  edges: Edge<CustomEdgeData>[];
} {
  const positions = calculateHierarchicalLayout(graph);

  const connectedInfo = selectedNodeId
    ? getConnectedNodeIds(selectedNodeId, graph.edges)
    : null;

  const nodes: Node<CustomNodeData>[] = graph.nodes.map(node => {
    const connectionCount = countConnections(node.id, graph.edges);
    const priority = calculatePriority(node, connectionCount);
    const position = positions.get(node.id) || { x: 0, y: 0 };

    let isConnected: boolean | undefined;
    let isSource = false;
    let isTarget = false;

    if (selectedNodeId && node.id !== selectedNodeId) {
      if (connectedInfo) {
        isConnected = connectedInfo.all.has(node.id);
        isSource = connectedInfo.sources.has(node.id);
        isTarget = connectedInfo.targets.has(node.id);
      }
    }

    return {
      id: node.id,
      type: 'custom',
      position,
      data: {
        name: node.name,
        type: node.type,
        health: node.health,
        priority,
        connectionCount,
        isConnected,
        isSource,
        isTarget,
      },
    };
  });

  const edges: Edge<CustomEdgeData>[] = graph.edges.map(edge => {
    let isActive: boolean | undefined;

    if (selectedNodeId) {
      if (edge.source === selectedNodeId || edge.target === selectedNodeId) {
        isActive = true;
      } else {
        isActive = false;
      }
    }

    return {
      id: edge.id,
      source: edge.source,
      target: edge.target,
      type: 'custom',
      data: {
        label: edge.label,
        isActive,
      },
    };
  });

  return { nodes, edges };
}

function GraphCanvasInner({
  graph,
  selectedNodeId,
  onNodeSelect,
  onEdgeClick,
  isLoading,
  error,
}: GraphCanvasProps) {
  const { fitView } = useReactFlow();
  const prevNodeCountRef = useRef(0);

  const { flowNodes, flowEdges } = useMemo(() => {
    if (!graph?.nodes) return { flowNodes: [], flowEdges: [] };
    const { nodes, edges } = transformGraphToFlow(graph, selectedNodeId);
    return { flowNodes: nodes, flowEdges: edges };
  }, [graph, selectedNodeId]);

  const [nodes, setNodes, onNodesChange] = useNodesState(flowNodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(flowEdges);

  useEffect(() => {
    setNodes(flowNodes);
    setEdges(flowEdges);
  }, [flowNodes, flowEdges, setNodes, setEdges]);

  useEffect(() => {
    const count = graph?.nodes?.length ?? 0;
    if (count > 0 && count !== prevNodeCountRef.current) {
      prevNodeCountRef.current = count;
      setTimeout(() => fitView({ padding: 0.15, duration: 500 }), 100);
    }
  }, [graph?.nodes?.length, fitView]);

  const nodesWithSelection = useMemo(() => {
    return nodes.map(node => ({
      ...node,
      selected: node.id === selectedNodeId,
    }));
  }, [nodes, selectedNodeId]);

  const handleNodeClick = useCallback(
    (_event: React.MouseEvent, node: Node) => {
      onNodeSelect(node.id);
    },
    [onNodeSelect]
  );

  const handleEdgeClick = useCallback(
    (_event: React.MouseEvent, edge: Edge) => {
      if (!onEdgeClick || !graph) return;
      const graphEdge = graph.edges.find(e => e.id === edge.id);
      if (graphEdge) onEdgeClick(graphEdge);
    },
    [onEdgeClick, graph]
  );

  const handlePaneClick = useCallback(() => {
    onNodeSelect(null);
  }, [onNodeSelect]);

  if (error) {
    return (
      <div className={styles.loading}>
        <div className={styles.loadingContent}>
          <div className={styles.errorIcon}>
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
              <circle cx="12" cy="12" r="10" />
              <path d="M12 8v4m0 4h.01" />
            </svg>
          </div>
          <span className={styles.loadingText}>Failed to load graph</span>
          <span className={styles.errorDetail}>{error.message}</span>
        </div>
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className={styles.loading}>
        <div className={styles.loadingContent}>
          <div className={styles.spinner} />
          <span className={styles.loadingText}>Loading graph...</span>
        </div>
      </div>
    );
  }

  if (graph && graph.nodes?.length === 0) {
    return <EmptyState />;
  }

  return (
    <div className={styles.canvas}>
      <ReactFlow
        nodes={nodesWithSelection}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onNodeClick={handleNodeClick}
        onEdgeClick={handleEdgeClick}
        onPaneClick={handlePaneClick}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        fitView
        fitViewOptions={{ padding: 0.15 }}
        minZoom={0.2}
        maxZoom={2.5}
        proOptions={{ hideAttribution: true }}
        defaultEdgeOptions={{ type: 'custom' }}
      >
        <Background
          variant={BackgroundVariant.Dots}
          gap={32}
          size={1}
          color="rgba(255, 255, 255, 0.03)"
        />
        <Controls
          className={styles.controls}
          showInteractive={false}
          position="bottom-right"
        />
      </ReactFlow>
    </div>
  );
}

export default function GraphCanvas(props: GraphCanvasProps) {
  return (
    <ReactFlowProvider>
      <GraphCanvasInner {...props} />
    </ReactFlowProvider>
  );
}
