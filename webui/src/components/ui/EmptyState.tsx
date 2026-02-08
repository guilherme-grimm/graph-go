import styles from './EmptyState.module.css';

export default function EmptyState() {
  return (
    <div className={styles.container}>
      <div className={styles.content}>
        <div className={styles.icon}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
            <circle cx="12" cy="12" r="3" />
            <path d="M12 2v4m0 12v4M2 12h4m12 0h4" />
            <path d="M4.93 4.93l2.83 2.83m8.48 8.48l2.83 2.83" />
            <path d="M19.07 4.93l-2.83 2.83m-8.48 8.48l-2.83 2.83" />
          </svg>
        </div>
        <h2 className={styles.title}>No nodes discovered</h2>
        <p className={styles.message}>
          Connect a data source in <code className={styles.code}>conf/config.yaml</code> to visualize your infrastructure.
        </p>
      </div>
    </div>
  );
}
