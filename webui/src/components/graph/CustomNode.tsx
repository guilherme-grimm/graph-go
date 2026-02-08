import React, { memo } from 'react';
import { Handle, Position, type Node, type NodeProps } from '@xyflow/react';
import type { HealthStatus, NodeType, PriorityTier } from '../../types';
import styles from './CustomNode.module.css';

export type CustomNodeData = Record<string, unknown> & {
  name: string;
  type: NodeType;
  health: HealthStatus;
  priority: PriorityTier;
  connectionCount: number;
  isConnected?: boolean;
  isSource?: boolean;
  isTarget?: boolean;
};

type CustomNodeType = Node<CustomNodeData>;

const NodeIcon = ({ type }: { type: NodeType }) => {
  const icons: Record<NodeType, React.ReactElement> = {
    database: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
        <ellipse cx="12" cy="6" rx="8" ry="3" />
        <path d="M4 6v6c0 1.657 3.582 3 8 3s8-1.343 8-3V6" />
        <path d="M4 12v6c0 1.657 3.582 3 8 3s8-1.343 8-3v-6" />
      </svg>
    ),
    bucket: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
        <path d="M4 7l2 13h12l2-13" />
        <path d="M3 7h18" />
        <path d="M8 7V5a4 4 0 018 0v2" />
      </svg>
    ),
    service: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
        <rect x="3" y="8" width="7" height="8" rx="1" />
        <rect x="14" y="8" width="7" height="8" rx="1" />
        <path d="M10 12h4" />
        <circle cx="12" cy="4" r="2" />
        <path d="M12 6v2" />
      </svg>
    ),
    api: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
        <path d="M4 6h16M4 12h16M4 18h10" />
        <circle cx="18" cy="18" r="3" />
        <path d="M18 16v4M16 18h4" />
      </svg>
    ),
    gateway: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
        <path d="M12 3L3 9v12h18V9l-9-6z" />
        <path d="M9 21v-6h6v6" />
        <path d="M3 9h18" />
      </svg>
    ),
    queue: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
        <rect x="3" y="5" width="18" height="4" rx="1" />
        <rect x="3" y="10" width="18" height="4" rx="1" />
        <rect x="3" y="15" width="18" height="4" rx="1" />
        <path d="M17 7h2M17 12h2M17 17h2" strokeLinecap="round" />
      </svg>
    ),
    cache: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
        <path d="M12 2L2 7l10 5 10-5-10-5z" />
        <path d="M2 17l10 5 10-5" />
        <path d="M2 12l10 5 10-5" />
      </svg>
    ),
    storage: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
        <rect x="4" y="4" width="16" height="6" rx="1" />
        <rect x="4" y="14" width="16" height="6" rx="1" />
        <circle cx="7" cy="7" r="1" fill="currentColor" />
        <circle cx="7" cy="17" r="1" fill="currentColor" />
      </svg>
    ),
    payment: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
        <rect x="2" y="5" width="20" height="14" rx="2" />
        <path d="M2 10h20" />
        <path d="M6 15h4" />
      </svg>
    ),
    auth: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
        <rect x="5" y="11" width="14" height="10" rx="2" />
        <path d="M12 16v2" />
        <path d="M8 11V7a4 4 0 118 0v4" />
      </svg>
    ),
    table: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
        <rect x="3" y="3" width="18" height="18" rx="2" />
        <path d="M3 9h18M9 3v18" />
      </svg>
    ),
    collection: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
        <rect x="4" y="4" width="6" height="6" rx="1" />
        <rect x="14" y="4" width="6" height="6" rx="1" />
        <rect x="4" y="14" width="6" height="6" rx="1" />
        <rect x="14" y="14" width="6" height="6" rx="1" />
      </svg>
    ),
  };

  return icons[type] || icons.service;
};

function CustomNode({ data, selected }: NodeProps<CustomNodeType>) {
  const { name, type, health, priority, isConnected } = data;
  const isDimmed = isConnected === false && !selected;
  const showPriorityBorder = priority === 'critical' || priority === 'high';

  return (
    <div
      className={`
        ${styles.node}
        ${selected ? styles.selected : ''}
        ${isDimmed ? styles.dimmed : ''}
        ${showPriorityBorder ? styles[`priority_${priority}`] : ''}
      `}
    >
      <Handle type="target" position={Position.Top} className={styles.handle} id="top" />
      <Handle type="source" position={Position.Bottom} className={styles.handle} id="bottom" />

      <div className={styles.card}>
        <span className={`${styles.healthDot} ${styles[`health_${health}`]}`} />
        <div className={styles.iconWrapper}>
          <NodeIcon type={type} />
        </div>
        <div className={styles.info}>
          <span className={styles.name} title={name}>{name}</span>
          <span className={styles.type}>{type}</span>
        </div>
      </div>
    </div>
  );
}

export default memo(CustomNode);
