import { useState, useCallback, useMemo, useEffect, useRef } from 'react';
import { useParams, useNavigate, useSearchParams } from 'react-router-dom';
import { GraphCanvas } from './graph';
import { NodeInspector, SearchOverlay, EdgeInspector } from './panels';
import HeaderBar, { type LayoutMode } from './HeaderBar';
import { ErrorBoundary, EmptyState } from './ui';
import { useWebSocket, useAppShortcuts } from '../hooks';
import { useGraph as useGraphData } from '../api';
import {
  hasNamespaces,
  applyFilters,
  collapseNamespaces,
  activeFilterCount as countActiveFilters,
  filtersFromParams,
  writeFiltersToParams,
  collapseFromParams,
  writeCollapseToParams,
  isCollapsed as isNamespaceCollapsed,
  toggleCollapsed,
  namespaceNames,
  namespaceOf,
  ALL_COLLAPSED,
  NO_COLLAPSE,
  EMPTY_FILTERS,
  LARGE_GRAPH_NODES,
  type CollapseState,
  type Filters,
} from '../utils';
import type { Graph, GraphEdge } from '../types';
import styles from './Layout.module.css';

export default function Layout() {
  const { nodeId: urlNodeId } = useParams<{ nodeId: string }>();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();

  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(urlNodeId ?? null);
  const [selectedEdge, setSelectedEdge] = useState<GraphEdge | null>(null);
  const [searchOpen, setSearchOpen] = useState(false);
  const [filters, setFilters] = useState<Filters>(() => filtersFromParams(searchParams));
  // null = nothing chosen yet; the default then depends on graph size.
  const [collapseChoice, setCollapseChoice] = useState<CollapseState | null>(() => collapseFromParams(searchParams));
  const [layoutNotice, setLayoutNotice] = useState<{ from: LayoutMode; nodeCount: number } | null>(null);
  const [layoutMode, setLayoutMode] = useState<LayoutMode>(
    () => (localStorage.getItem('graph-layout-mode') as LayoutMode) || 'hierarchical'
  );
  const [layoutResetKey, setLayoutResetKey] = useState(0);

  // Persist layout mode to localStorage
  const handleLayoutChange = useCallback((mode: LayoutMode) => {
    setLayoutMode(mode);
    setLayoutNotice(null);
    localStorage.setItem('graph-layout-mode', mode);
  }, []);

  const handleResetPositions = useCallback(() => {
    setLayoutResetKey(k => k + 1);
  }, []);

  const handleFilterChange = useCallback((newFilters: Filters) => {
    setFilters(newFilters);
    setSearchParams(prev => {
      const params = new URLSearchParams(prev);
      writeFiltersToParams(newFilters, params);
      return params;
    }, { replace: true });
  }, [setSearchParams]);

  const { data: apiGraph, isLoading, error, refetch } = useGraphData();
  const graph: Graph | undefined = apiGraph;

  const nodeCount = graph?.nodes?.length ?? 0;
  const isLargeGraph = nodeCount > LARGE_GRAPH_NODES;
  const graphHasNamespaces = useMemo(() => (graph ? hasNamespaces(graph) : false), [graph]);

  // Auto-detect swimlane mode for k8s graphs when no explicit layout was
  // saved, and switch to it once a k8s graph gets large: other layouts turn
  // into a haystack there and can stall the tab. Runs once per page load, so
  // switching back by hand sticks.
  const autoDetectedRef = useRef(false);
  useEffect(() => {
    if (autoDetectedRef.current || !nodeCount) return;
    autoDetectedRef.current = true;
    if (!graphHasNamespaces || layoutMode === 'swimlane') return;
    const savedMode = localStorage.getItem('graph-layout-mode');
    if (!savedMode) {
      setLayoutMode('swimlane');
      localStorage.setItem('graph-layout-mode', 'swimlane');
    } else if (isLargeGraph) {
      setLayoutMode('swimlane');
      setLayoutNotice({ from: layoutMode, nodeCount });
    }
  }, [nodeCount, graphHasNamespaces, isLargeGraph, layoutMode]);

  // Large clusters start with every namespace collapsed unless the URL says
  // otherwise.
  const collapse = collapseChoice ?? (isLargeGraph && graphHasNamespaces ? ALL_COLLAPSED : NO_COLLAPSE);
  const collapseRef = useRef(collapse);
  useEffect(() => {
    collapseRef.current = collapse;
  }, [collapse]);

  const updateCollapse = useCallback((next: CollapseState) => {
    setCollapseChoice(next);
    setSearchParams(prev => {
      const params = new URLSearchParams(prev);
      writeCollapseToParams(next, params);
      return params;
    }, { replace: true });
  }, [setSearchParams]);

  const handleToggleCollapse = useCallback((nsName: string) => {
    updateCollapse(toggleCollapsed(collapseRef.current, nsName));
  }, [updateCollapse]);

  const handleCollapseAll = useCallback((collapsed: boolean) => {
    updateCollapse(collapsed ? ALL_COLLAPSED : NO_COLLAPSE);
  }, [updateCollapse]);

  const isCollapsed = useCallback((nsName: string) => isNamespaceCollapsed(collapse, nsName), [collapse]);

  const { status: wsStatus } = useWebSocket();

  useAppShortcuts({
    onSearch: useCallback(() => setSearchOpen(true), []),
    onEscape: useCallback(() => {
      if (searchOpen) {
        setSearchOpen(false);
      } else if (selectedEdge) {
        setSelectedEdge(null);
        setSearchParams(prev => {
          const params = new URLSearchParams(prev);
          params.delete('edge');
          return params;
        }, { replace: true });
      } else if (selectedNodeId) {
        setSelectedNodeId(null);
        navigate({ pathname: '/', search: window.location.search }, { replace: true });
      }
    }, [searchOpen, selectedEdge, selectedNodeId, navigate, setSearchParams]),
  });

  // Node selection changes the path; the query string carries filters and
  // collapse state, so it is kept (read live, since setSearchParams may have
  // just updated it in the same handler).
  const handleNodeSelect = useCallback((nodeId: string | null) => {
    setSelectedNodeId(nodeId);
    setSelectedEdge(null);
    if (nodeId) {
      navigate({ pathname: `/node/${nodeId}`, search: window.location.search }, { replace: true });
    } else {
      navigate({ pathname: '/', search: window.location.search }, { replace: true });
    }
  }, [navigate]);

  const handleSearchSelect = useCallback((nodeId: string) => {
    // Jumping to something inside a collapsed namespace opens that namespace.
    if (graph) {
      const nodeById = new Map(graph.nodes.map(n => [n.id, n]));
      const node = nodeById.get(nodeId);
      const nsId = node && node.type !== 'namespace' ? namespaceOf(node, nodeById) : undefined;
      const nsName = nsId ? nodeById.get(nsId)?.name : undefined;
      if (nsName && isNamespaceCollapsed(collapseRef.current, nsName)) {
        updateCollapse(toggleCollapsed(collapseRef.current, nsName));
      }
    }
    setSelectedNodeId(nodeId);
    setSelectedEdge(null);
    setSearchOpen(false);
    navigate({ pathname: `/node/${nodeId}`, search: window.location.search }, { replace: true });
  }, [navigate, graph, updateCollapse]);

  const handleEdgeClick = useCallback((edge: GraphEdge) => {
    setSelectedEdge(edge);
    setSelectedNodeId(null);
    setSearchParams(prev => {
      const params = new URLSearchParams(prev);
      params.set('edge', edge.id);
      return params;
    }, { replace: true });
  }, [setSearchParams]);

  // Restore edge selection from URL on mount
  useEffect(() => {
    const edgeParam = searchParams.get('edge');
    if (edgeParam && graph?.edges && !selectedEdge) {
      const edge = graph.edges.find(e => e.id === edgeParam);
      if (edge) setSelectedEdge(edge);
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [graph?.edges]);

  // Filter, then collapse, for the canvas; NodeInspector keeps the full graph.
  const filteredGraph = useMemo(() => (graph?.nodes ? applyFilters(graph, filters) : undefined), [graph, filters]);
  const view = useMemo(
    () => (filteredGraph ? collapseNamespaces(filteredGraph, collapse) : undefined),
    [filteredGraph, collapse],
  );
  const namespaces = useMemo(() => namespaceNames(graph), [graph]);

  const activeFilterCount = countActiveFilters(filters);
  const isFilteredEmpty = activeFilterCount > 0 && filteredGraph?.nodes?.length === 0 && nodeCount > 0;

  const handleClearFilters = useCallback(() => {
    handleFilterChange(EMPTY_FILTERS);
  }, [handleFilterChange]);

  return (
    <div className={styles.layout}>
      <HeaderBar
        graph={graph}
        filteredGraph={filteredGraph}
        onSearchOpen={() => setSearchOpen(true)}
        filters={filters}
        onFilterChange={handleFilterChange}
        layoutMode={layoutMode}
        onLayoutChange={handleLayoutChange}
        onResetPositions={handleResetPositions}
        wsStatus={wsStatus}
        namespaces={namespaces}
        isCollapsed={isCollapsed}
        onToggleCollapse={handleToggleCollapse}
        onCollapseAll={handleCollapseAll}
        layoutNotice={layoutNotice}
        onDismissLayoutNotice={() => setLayoutNotice(null)}
        isLargeGraph={isLargeGraph}
      />

      <div className={styles.graphArea}>
        <ErrorBoundary>
          {isFilteredEmpty ? (
            <EmptyState
              reason="filtered"
              filterCount={activeFilterCount}
              onClearFilters={handleClearFilters}
            />
          ) : (
            <GraphCanvas
              graph={view?.graph}
              namespaces={view?.namespaces}
              onToggleCollapse={handleToggleCollapse}
              selectedNodeId={selectedNodeId}
              onNodeSelect={handleNodeSelect}
              onEdgeClick={handleEdgeClick}
              layoutMode={layoutMode}
              resetKey={layoutResetKey}
              isLoading={isLoading}
              error={error instanceof Error ? error : error ? new Error(String(error)) : null}
              onRetry={() => refetch()}
            />
          )}
        </ErrorBoundary>
      </div>

      <NodeInspector
        nodeId={selectedNodeId}
        onClose={() => handleNodeSelect(null)}
        graph={graph}
        onNodeSelect={handleNodeSelect}
      />

      <EdgeInspector
        edge={selectedEdge}
        onClose={() => {
          setSelectedEdge(null);
          setSearchParams(prev => {
            const params = new URLSearchParams(prev);
            params.delete('edge');
            return params;
          }, { replace: true });
        }}
        graph={graph}
        onNodeSelect={handleNodeSelect}
      />

      <SearchOverlay
        isOpen={searchOpen}
        onClose={() => setSearchOpen(false)}
        onNodeSelect={handleSearchSelect}
      />
    </div>
  );
}
