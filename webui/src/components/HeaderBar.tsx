import { useMemo, useState } from 'react';
import type { Graph, NodeType, HealthStatus } from '../types';
import styles from './HeaderBar.module.css';

export interface Filters {
  types: NodeType[];
  health: HealthStatus[];
}

export type LayoutMode = 'hierarchical' | 'force';

interface HeaderBarProps {
  graph?: Graph;
  onSearchOpen: () => void;
  filters: Filters;
  onFilterChange: (filters: Filters) => void;
  layoutMode: LayoutMode;
  onLayoutChange: (mode: LayoutMode) => void;
}

const nodeTypes: NodeType[] = [
  'database', 'table', 'collection', 'bucket', 'storage',
  'service', 'api', 'gateway', 'queue', 'cache', 'payment', 'auth',
];

const healthStatuses: HealthStatus[] = ['healthy', 'degraded', 'unhealthy'];

export default function HeaderBar({ graph, onSearchOpen, filters, onFilterChange, layoutMode, onLayoutChange }: HeaderBarProps) {
  const [dropdownOpen, setDropdownOpen] = useState(false);
  const healthCounts = useMemo(() => {
    if (!graph?.nodes) return { healthy: 0, degraded: 0, unhealthy: 0, unknown: 0, total: 0 };

    const counts = { healthy: 0, degraded: 0, unhealthy: 0, unknown: 0 };
    graph.nodes.forEach(node => {
      counts[node.health]++;
    });

    return { ...counts, total: graph.nodes.length };
  }, [graph]);

  // Only show type chips that exist in the current graph
  const activeTypes = useMemo(() => {
    if (!graph?.nodes) return [];
    const typesInGraph = new Set(graph.nodes.map(n => n.type));
    return nodeTypes.filter(t => typesInGraph.has(t));
  }, [graph]);

  const toggleType = (type: NodeType) => {
    const types = filters.types.includes(type)
      ? filters.types.filter(t => t !== type)
      : [...filters.types, type];
    onFilterChange({ ...filters, types });
  };

  const toggleHealth = (status: HealthStatus) => {
    const health = filters.health.includes(status)
      ? filters.health.filter(s => s !== status)
      : [...filters.health, status];
    onFilterChange({ ...filters, health });
  };

  return (
    <header className={styles.header}>
      <span className={styles.appName}>graph-info</span>

      <button className={styles.searchTrigger} onClick={onSearchOpen}>
        <svg className={styles.searchIcon} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <circle cx="11" cy="11" r="8" />
          <path d="M21 21l-4.35-4.35" />
        </svg>
        <span className={styles.searchLabel}>Search nodes...</span>
        <kbd className={styles.searchKbd}>Cmd+K</kbd>
      </button>

      <div className={styles.layoutToggle}>
        <button
          className={styles.layoutButton}
          onClick={() => setDropdownOpen(!dropdownOpen)}
        >
          Layout: {layoutMode === 'hierarchical' ? 'Hierarchical' : 'Force-Directed'}
          <svg className={styles.chevron} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
            <path d="M6 9l6 6 6-6" />
          </svg>
        </button>
        {dropdownOpen && (
          <div className={styles.dropdown}>
            <button
              className={`${styles.dropdownItem} ${layoutMode === 'hierarchical' ? styles.dropdownItemActive : ''}`}
              onClick={() => {
                onLayoutChange('hierarchical');
                setDropdownOpen(false);
              }}
            >
              Hierarchical (Top-Down)
            </button>
            <button
              className={`${styles.dropdownItem} ${layoutMode === 'force' ? styles.dropdownItemActive : ''}`}
              onClick={() => {
                onLayoutChange('force');
                setDropdownOpen(false);
              }}
            >
              Force-Directed (Organic)
            </button>
          </div>
        )}
      </div>

      {activeTypes.length > 0 && (
        <div className={styles.filterGroup}>
          {activeTypes.map(type => (
            <button
              key={type}
              className={`${styles.filterChip} ${filters.types.includes(type) ? styles.chipActive : ''}`}
              onClick={() => toggleType(type)}
            >
              {type}
            </button>
          ))}
          {healthStatuses.map(status => (
            <button
              key={status}
              className={`${styles.filterChip} ${styles[`chip_${status}`]} ${filters.health.includes(status) ? styles.chipActive : ''}`}
              onClick={() => toggleHealth(status)}
            >
              <span className={`${styles.chipDot} ${styles[`chipDot_${status}`]}`} />
              {status}
            </button>
          ))}
        </div>
      )}

      <div className={styles.healthSummary}>
        {healthCounts.healthy > 0 && (
          <span className={styles.healthBadge}>
            <span className={`${styles.dot} ${styles.dotHealthy}`} />
            {healthCounts.healthy}
          </span>
        )}
        {healthCounts.degraded > 0 && (
          <span className={styles.healthBadge}>
            <span className={`${styles.dot} ${styles.dotDegraded}`} />
            {healthCounts.degraded}
          </span>
        )}
        {healthCounts.unhealthy > 0 && (
          <span className={styles.healthBadge}>
            <span className={`${styles.dot} ${styles.dotUnhealthy}`} />
            {healthCounts.unhealthy}
          </span>
        )}
        <span className={styles.totalCount}>{healthCounts.total} nodes</span>
      </div>
    </header>
  );
}
