import { useEffect, useRef, type ReactNode } from 'react';
import styles from './Panel.module.css';

interface PanelProps {
  isOpen: boolean;
  onClose: () => void;
  children: ReactNode;
  position?: 'right' | 'bottom';
  width?: string;
}

export default function Panel({
  isOpen,
  onClose,
  children,
  position = 'right',
  width = '360px',
}: PanelProps) {
  const panelRef = useRef<HTMLDivElement>(null);

  // Handle click outside
  useEffect(() => {
    if (!isOpen) return;

    function handleClickOutside(event: MouseEvent) {
      if (panelRef.current && !panelRef.current.contains(event.target as Node)) {
        onClose();
      }
    }

    // Delay to prevent immediate close
    const timeoutId = setTimeout(() => {
      document.addEventListener('mousedown', handleClickOutside);
    }, 100);

    return () => {
      clearTimeout(timeoutId);
      document.removeEventListener('mousedown', handleClickOutside);
    };
  }, [isOpen, onClose]);

  // Handle escape key
  useEffect(() => {
    if (!isOpen) return;

    function handleEscape(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        onClose();
      }
    }

    document.addEventListener('keydown', handleEscape);
    return () => document.removeEventListener('keydown', handleEscape);
  }, [isOpen, onClose]);

  if (!isOpen) return null;

  return (
    <div
      ref={panelRef}
      className={`${styles.panel} ${styles[position]}`}
      style={{ '--panel-width': width } as React.CSSProperties}
      role="dialog"
      aria-modal="true"
    >
      {children}
    </div>
  );
}
