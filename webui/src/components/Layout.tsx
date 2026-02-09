import { useState, useCallback, useMemo } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { GraphCanvas } from './graph';
import { NodeInspector, SearchOverlay, EdgeInspector } from './panels';
import HeaderBar, { type Filters, type LayoutMode } from './HeaderBar';
import { ErrorBoundary } from './ui';
import { useWebSocket, useAppShortcuts } from '../hooks';
import { useGraph as useGraphData } from '../api';
import { MOCK_GRAPH } from '../data';
import type { Graph, GraphEdge } from '../types';
import styles from './Layout.module.css';

export default function Layout() {
  const { nodeId: urlNodeId } = useParams<{ nodeId: string }>();
  const navigate = useNavigate();

  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(urlNodeId ?? null);
  const [selectedEdge, setSelectedEdge] = useState<GraphEdge | null>(null);
  const [searchOpen, setSearchOpen] = useState(false);
  const [filters, setFilters] = useState<Filters>({ types: [], health: [] });
  const [layoutMode, setLayoutMode] = useState<LayoutMode>(
    () => (localStorage.getItem('graph-layout-mode') as LayoutMode) || 'hierarchical'
  );

  // Persist layout mode to localStorage
  const handleLayoutChange = useCallback((mode: LayoutMode) => {
    setLayoutMode(mode);
    localStorage.setItem('graph-layout-mode', mode);
  }, []);

  const { data: apiGraph, isLoading, error } = useGraphData();
  const isMockData = !apiGraph?.nodes;
  const graph: Graph = isMockData ? MOCK_GRAPH : apiGraph;

  useWebSocket();

  useAppShortcuts({
    onSearch: useCallback(() => setSearchOpen(true), []),
    onEscape: useCallback(() => {
      if (searchOpen) {
        setSearchOpen(false);
      } else if (selectedEdge) {
        setSelectedEdge(null);
      } else if (selectedNodeId) {
        setSelectedNodeId(null);
        navigate('/', { replace: true });
      }
    }, [searchOpen, selectedEdge, selectedNodeId, navigate]),
  });

  const handleNodeSelect = useCallback((nodeId: string | null) => {
    setSelectedNodeId(nodeId);
    setSelectedEdge(null);
    if (nodeId) {
      navigate(`/node/${nodeId}`, { replace: true });
    } else {
      navigate('/', { replace: true });
    }
  }, [navigate]);

  const handleSearchSelect = useCallback((nodeId: string) => {
    setSelectedNodeId(nodeId);
    setSelectedEdge(null);
    setSearchOpen(false);
    navigate(`/node/${nodeId}`, { replace: true });
  }, [navigate]);

  const handleEdgeClick = useCallback((edge: GraphEdge) => {
    setSelectedEdge(edge);
    setSelectedNodeId(null);
    navigate('/', { replace: true });
  }, [navigate]);

  // Filter graph for the canvas while keeping unfiltered graph for NodeInspector
  const filteredGraph = useMemo((): Graph | undefined => {
    if (!graph?.nodes) return undefined;
    if (filters.types.length === 0 && filters.health.length === 0) return graph;

    const filteredNodes = graph.nodes.filter(node => {
      if (filters.types.length > 0 && !filters.types.includes(node.type)) return false;
      if (filters.health.length > 0 && !filters.health.includes(node.health)) return false;
      return true;
    });

    const nodeIds = new Set(filteredNodes.map(n => n.id));
    const filteredEdges = graph.edges.filter(
      edge => nodeIds.has(edge.source) && nodeIds.has(edge.target)
    );

    return { nodes: filteredNodes, edges: filteredEdges };
  }, [graph, filters]);

  return (
    <div className={styles.layout}>
      <HeaderBar
        graph={graph}
        onSearchOpen={() => setSearchOpen(true)}
        filters={filters}
        onFilterChange={setFilters}
        layoutMode={layoutMode}
        onLayoutChange={handleLayoutChange}
      />

      {isMockData && !isLoading && (
        <div className={styles.mockBanner}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" className={styles.mockIcon}>
            <path d="M12 9v4m0 4h.01M12 2L2 22h20L12 2z" />
          </svg>
          <span>Displaying mock data — backend unavailable</span>
        </div>
      )}

      <div className={styles.graphArea}>
        <ErrorBoundary>
          <GraphCanvas
            graph={filteredGraph}
            selectedNodeId={selectedNodeId}
            onNodeSelect={handleNodeSelect}
            onEdgeClick={handleEdgeClick}
            layoutMode={layoutMode}
            isLoading={!apiGraph && isLoading}
            error={error instanceof Error ? error : error ? new Error(String(error)) : null}
          />
        </ErrorBoundary>
      </div>

      <NodeInspector
        nodeId={selectedNodeId}
        onClose={() => handleNodeSelect(null)}
        graph={graph}
      />

      <EdgeInspector
        edge={selectedEdge}
        onClose={() => setSelectedEdge(null)}
        graph={graph}
      />

      <SearchOverlay
        isOpen={searchOpen}
        onClose={() => setSearchOpen(false)}
        onNodeSelect={handleSearchSelect}
      />
    </div>
  );
}
