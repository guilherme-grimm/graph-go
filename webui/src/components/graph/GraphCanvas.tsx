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
import type { Graph, GraphEdge } from '../../types';
import { calculatePriority, countConnections, calculateHierarchicalLayout, calculateForceDirectedLayout, debounce } from '../../utils';
import { useLayoutPersistence } from '../../hooks';
import type { LayoutMode } from '../HeaderBar';
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
  layoutMode: LayoutMode;
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

function transformGraphToFlow(
  graph: Graph,
  layoutMode: LayoutMode,
  savedPositions: Map<string, { x: number; y: number; isPinned?: boolean }>,
  selectedNodeId: string | null,
  viewportSize: { width: number; height: number }
): {
  nodes: Node<CustomNodeData>[];
  edges: Edge<CustomEdgeData>[];
} {
  // Calculate base layout positions
  const basePositions = layoutMode === 'hierarchical'
    ? calculateHierarchicalLayout(graph)
    : calculateForceDirectedLayout(graph, viewportSize);

  // Merge with saved positions (pinned nodes override calculated positions)
  const finalPositions = new Map(basePositions);
  savedPositions.forEach((pos, nodeId) => {
    if (pos.isPinned && basePositions.has(nodeId)) {
      finalPositions.set(nodeId, { x: pos.x, y: pos.y });
    }
  });

  const connectedInfo = selectedNodeId
    ? getConnectedNodeIds(selectedNodeId, graph.edges)
    : null;

  const nodes: Node<CustomNodeData>[] = graph.nodes.map(node => {
    const connectionCount = countConnections(node.id, graph.edges);
    const priority = calculatePriority(node, connectionCount);
    const position = finalPositions.get(node.id) || { x: 0, y: 0 };
    const savedPos = savedPositions.get(node.id);

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
        isPinned: savedPos?.isPinned || false,
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
  layoutMode,
  isLoading,
  error,
}: GraphCanvasProps) {
  const { fitView } = useReactFlow();
  const prevNodeCountRef = useRef(0);
  const { savedPositions, savePosition } = useLayoutPersistence(graph);

  // Use a fixed viewport size for force-directed layout
  const viewportSize = { width: 1200, height: 800 };

  const { flowNodes, flowEdges } = useMemo(() => {
    if (!graph?.nodes) return { flowNodes: [], flowEdges: [] };
    const { nodes, edges } = transformGraphToFlow(graph, layoutMode, savedPositions, selectedNodeId, viewportSize);
    return { flowNodes: nodes, flowEdges: edges };
  }, [graph, layoutMode, savedPositions, selectedNodeId]);

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

  // Debounced save position handler
  const debouncedSavePosition = useMemo(
    () => debounce((...args: unknown[]) => {
      const [nodeId, position] = args as [string, { x: number; y: number }];
      savePosition(nodeId, position, true);
    }, 500),
    [savePosition]
  );

  const handleNodeDragStop = useCallback(
    (_event: React.MouseEvent, node: Node) => {
      debouncedSavePosition(node.id, node.position);
    },
    [debouncedSavePosition]
  );

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
        onNodeDragStop={handleNodeDragStop}
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
