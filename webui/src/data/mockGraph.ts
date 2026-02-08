import type { Graph } from '../types';

export const MOCK_GRAPH: Graph = {
  nodes: [
    // Critical tier - Payment & Auth
    {
      id: 'payment-1',
      type: 'payment',
      name: 'Payment Gateway',
      metadata: { priority: 'critical', provider: 'Stripe', version: '3.2.1' },
      health: 'healthy',
    },
    {
      id: 'auth-1',
      type: 'auth',
      name: 'Auth Service',
      metadata: { priority: 'critical', provider: 'OAuth2', sessions: '12.4k' },
      health: 'healthy',
    },

    // High tier - Core infrastructure
    {
      id: 'api-1',
      type: 'api',
      name: 'API Gateway',
      metadata: { endpoints: ['/v1/users', '/v1/orders', '/v1/products'], version: '2.8.0' },
      health: 'healthy',
    },
    {
      id: 'db-1',
      type: 'database',
      name: 'PostgreSQL Primary',
      metadata: { tables: ['users', 'orders', 'products', 'transactions'], version: '15.2', size: '48GB' },
      health: 'healthy',
    },
    {
      id: 'db-2',
      type: 'database',
      name: 'PostgreSQL Replica',
      parent: 'db-1',
      metadata: { tables: ['users', 'orders', 'products', 'transactions'], version: '15.2', replica: true },
      health: 'healthy',
    },

    // Medium tier - Services
    {
      id: 'svc-user',
      type: 'service',
      name: 'User Service',
      parent: 'api-1',
      metadata: { version: '2.1.0', instances: 3 },
      health: 'healthy',
    },
    {
      id: 'svc-order',
      type: 'service',
      name: 'Order Service',
      parent: 'api-1',
      metadata: { version: '1.8.3', instances: 4 },
      health: 'degraded',
    },
    {
      id: 'svc-notify',
      type: 'service',
      name: 'Notification Service',
      metadata: { version: '1.2.0', instances: 2 },
      health: 'unhealthy',
    },
    {
      id: 'cache-1',
      type: 'cache',
      name: 'Redis Cluster',
      metadata: { size: '8GB', region: 'us-east-1', nodes: 3 },
      health: 'healthy',
    },
    {
      id: 'queue-1',
      type: 'queue',
      name: 'Event Queue',
      metadata: { pending: '2.3k', throughput: '450/s' },
      health: 'healthy',
    },

    // Low tier - Storage & auxiliary
    {
      id: 'bucket-1',
      type: 'bucket',
      name: 'Asset Storage',
      metadata: { region: 'us-east-1', size: '250GB', objects: '1.2M' },
      health: 'healthy',
    },
    {
      id: 'bucket-2',
      type: 'bucket',
      name: 'Backup Storage',
      metadata: { region: 'us-west-2', size: '1.8TB', retention: '90d' },
      health: 'healthy',
    },
  ],
  edges: [
    // API Gateway connections
    { id: 'e1', source: 'api-1', target: 'svc-user', type: 'routes', label: 'REST' },
    { id: 'e2', source: 'api-1', target: 'svc-order', type: 'routes', label: 'REST' },
    { id: 'e3', source: 'api-1', target: 'auth-1', type: 'validates', label: 'JWT' },

    // Service to database
    { id: 'e4', source: 'svc-user', target: 'db-1', type: 'queries', label: 'SQL' },
    { id: 'e5', source: 'svc-order', target: 'db-1', type: 'queries', label: 'SQL' },
    { id: 'e6', source: 'payment-1', target: 'db-1', type: 'queries', label: 'SQL' },

    // Database replication
    { id: 'e7', source: 'db-1', target: 'db-2', type: 'replicates' },

    // Caching
    { id: 'e8', source: 'svc-user', target: 'cache-1', type: 'caches' },
    { id: 'e9', source: 'auth-1', target: 'cache-1', type: 'sessions' },

    // Payment flow
    { id: 'e10', source: 'svc-order', target: 'payment-1', type: 'charges' },

    // Event queue
    { id: 'e11', source: 'svc-order', target: 'queue-1', type: 'publishes' },
    { id: 'e12', source: 'svc-notify', target: 'queue-1', type: 'subscribes' },
    { id: 'e13', source: 'payment-1', target: 'queue-1', type: 'publishes' },

    // Storage
    { id: 'e14', source: 'svc-user', target: 'bucket-1', type: 'uploads' },
    { id: 'e15', source: 'db-1', target: 'bucket-2', type: 'backups' },
  ],
};
