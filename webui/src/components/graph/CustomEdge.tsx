import { memo } from 'react';
import { BaseEdge, getBezierPath, type Edge, type EdgeProps } from '@xyflow/react';
import styles from './CustomEdge.module.css';

export type CustomEdgeData = Record<string, unknown> & {
  label?: string;
  isActive?: boolean;
  isHighlighted?: boolean;
};

type CustomEdgeType = Edge<CustomEdgeData>;

function CustomEdge({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  data,
  selected,
}: EdgeProps<CustomEdgeType>) {
  const [edgePath, labelX, labelY] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
    curvature: 0.25,
  });

  const isActive = data?.isActive;
  const isHighlighted = data?.isHighlighted || selected;

  return (
    <g className={styles.edgeGroup}>
      <BaseEdge
        id={id}
        path={edgePath}
        className={`
          ${styles.edge}
          ${isActive ? styles.active : ''}
          ${isHighlighted ? styles.highlighted : ''}
        `}
      />

      {/* Animated overlay for active edges */}
      {isActive && (
        <path
          d={edgePath}
          className={styles.animatedPath}
          fill="none"
          strokeDasharray="5 5"
        />
      )}

      {data?.label && (
        <g transform={`translate(${labelX}, ${labelY})`}>
          <rect
            x="-20"
            y="-10"
            width="40"
            height="20"
            rx="4"
            className={`${styles.labelBg} ${isHighlighted ? styles.labelBgActive : ''}`}
          />
          <text
            className={styles.labelText}
            textAnchor="middle"
            dominantBaseline="middle"
          >
            {data.label}
          </text>
        </g>
      )}
    </g>
  );
}

export default memo(CustomEdge);
