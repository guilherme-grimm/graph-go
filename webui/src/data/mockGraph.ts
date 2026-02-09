import type { Graph } from '../types';

export const MOCK_GRAPH: Graph = {
	nodes: [
		// ── Application Services ─────────────────────────────────────────────
		{
			id: 'api-gateway',
			type: 'gateway',
			name: 'api-gateway',
			metadata: { adapter: 'api-gateway', endpoint: 'http://api-gateway:8080' },
			health: 'healthy',
		},
		{
			id: 'user-service',
			type: 'auth',
			name: 'user-service',
			metadata: { adapter: 'user-service', endpoint: 'http://user-service:8080' },
			health: 'healthy',
		},
		{
			id: 'order-service',
			type: 'service',
			name: 'order-service',
			metadata: { adapter: 'order-service', endpoint: 'http://order-service:8080' },
			health: 'healthy',
		},
		{
			id: 'product-service',
			type: 'service',
			name: 'product-service',
			metadata: { adapter: 'product-service', endpoint: 'http://product-service:8080' },
			health: 'healthy',
		},
		{
			id: 'media-service',
			type: 'service',
			name: 'media-service',
			metadata: { adapter: 'media-service', endpoint: 'http://media-service:8080' },
			health: 'healthy',
		},

		// ── Infrastructure: service-level nodes ──────────────────────────────
		{
			id: 'service-postgres',
			type: 'postgres',
			name: 'postgres',
			metadata: { adapter: 'postgres' },
			health: 'healthy',
		},
		{
			id: 'service-mongodb',
			type: 'mongodb',
			name: 'mongodb',
			metadata: { adapter: 'mongodb' },
			health: 'healthy',
		},
		{
			id: 'service-minio',
			type: 's3',
			name: 'minio',
			metadata: { adapter: 'minio' },
			health: 'healthy',
		},

		// PostgreSQL: database -> tables
		{
			id: 'pg-mydb',
			type: 'database',
			name: 'mydb',
			parent: 'service-postgres',
			metadata: { adapter: 'postgres', database: 'mydb' },
			health: 'healthy',
		},
		{
			id: 'pg-mydb-users',
			type: 'table',
			name: 'users',
			parent: 'pg-mydb',
			metadata: { adapter: 'postgres', schema: 'public' },
			health: 'healthy',
		},
		{
			id: 'pg-mydb-products',
			type: 'table',
			name: 'products',
			parent: 'pg-mydb',
			metadata: { adapter: 'postgres', schema: 'public' },
			health: 'healthy',
		},
		{
			id: 'pg-mydb-orders',
			type: 'table',
			name: 'orders',
			parent: 'pg-mydb',
			metadata: { adapter: 'postgres', schema: 'public' },
			health: 'healthy',
		},
		{
			id: 'pg-mydb-order_items',
			type: 'table',
			name: 'order_items',
			parent: 'pg-mydb',
			metadata: { adapter: 'postgres', schema: 'public' },
			health: 'healthy',
		},

		// MongoDB: database -> collections
		{
			id: 'mongo-store',
			type: 'database',
			name: 'store',
			parent: 'service-mongodb',
			metadata: { adapter: 'mongodb' },
			health: 'healthy',
		},
		{
			id: 'mongo-store-products',
			type: 'collection',
			name: 'products',
			parent: 'mongo-store',
			metadata: { adapter: 'mongodb', database: 'store' },
			health: 'healthy',
		},
		{
			id: 'mongo-store-reviews',
			type: 'collection',
			name: 'reviews',
			parent: 'mongo-store',
			metadata: { adapter: 'mongodb', database: 'store' },
			health: 'healthy',
		},
		{
			id: 'mongo-store-categories',
			type: 'collection',
			name: 'categories',
			parent: 'mongo-store',
			metadata: { adapter: 'mongodb', database: 'store' },
			health: 'healthy',
		},

		// S3/MinIO: buckets -> prefixes
		{
			id: 's3-assets',
			type: 'bucket',
			name: 'assets',
			parent: 'service-minio',
			metadata: { adapter: 's3' },
			health: 'healthy',
		},
		{
			id: 's3-assets-images/',
			type: 'storage',
			name: 'images/',
			parent: 's3-assets',
			metadata: { adapter: 's3', bucket: 'assets' },
			health: 'healthy',
		},
		{
			id: 's3-assets-documents/',
			type: 'storage',
			name: 'documents/',
			parent: 's3-assets',
			metadata: { adapter: 's3', bucket: 'assets' },
			health: 'healthy',
		},
		{
			id: 's3-backups',
			type: 'bucket',
			name: 'backups',
			parent: 'service-minio',
			metadata: { adapter: 's3' },
			health: 'healthy',
		},
		{
			id: 's3-backups-daily/',
			type: 'storage',
			name: 'daily/',
			parent: 's3-backups',
			metadata: { adapter: 's3', bucket: 'backups' },
			health: 'healthy',
		},
		{
			id: 's3-backups-weekly/',
			type: 'storage',
			name: 'weekly/',
			parent: 's3-backups',
			metadata: { adapter: 's3', bucket: 'backups' },
			health: 'healthy',
		},
	],
	edges: [
		// ── Gateway → Services ───────────────────────────────────────────────
		{ id: 'api-gateway-to-user-service', source: 'api-gateway', target: 'user-service', type: 'depends_on', label: 'routes' },
		{ id: 'api-gateway-to-order-service', source: 'api-gateway', target: 'order-service', type: 'depends_on', label: 'routes' },
		{ id: 'api-gateway-to-product-service', source: 'api-gateway', target: 'product-service', type: 'depends_on', label: 'routes' },
		{ id: 'api-gateway-to-media-service', source: 'api-gateway', target: 'media-service', type: 'depends_on', label: 'routes' },

		// ── Services → Infrastructure ────────────────────────────────────────
		{ id: 'user-service-to-pg-mydb-users', source: 'user-service', target: 'pg-mydb-users', type: 'depends_on', label: 'reads/writes' },
		{ id: 'order-service-to-pg-mydb-orders', source: 'order-service', target: 'pg-mydb-orders', type: 'depends_on', label: 'reads/writes' },
		{ id: 'order-service-to-pg-mydb-order_items', source: 'order-service', target: 'pg-mydb-order_items', type: 'depends_on', label: 'reads/writes' },
		{ id: 'product-service-to-pg-mydb-products', source: 'product-service', target: 'pg-mydb-products', type: 'depends_on', label: 'reads/writes' },
		{ id: 'product-service-to-mongo-store-products', source: 'product-service', target: 'mongo-store-products', type: 'depends_on', label: 'reads/writes' },
		{ id: 'product-service-to-mongo-store-categories', source: 'product-service', target: 'mongo-store-categories', type: 'depends_on', label: 'reads' },
		{ id: 'media-service-to-s3-assets', source: 'media-service', target: 's3-assets', type: 'depends_on', label: 'reads/writes' },

		// ── Infrastructure contains edges ────────────────────────────────────
		{ id: 'svc-pg-mydb', source: 'service-postgres', target: 'pg-mydb', type: 'contains', label: 'contains' },
		{ id: 'svc-mongo-store', source: 'service-mongodb', target: 'mongo-store', type: 'contains', label: 'contains' },
		{ id: 'svc-minio-assets', source: 'service-minio', target: 's3-assets', type: 'contains', label: 'contains' },
		{ id: 'svc-minio-backups', source: 'service-minio', target: 's3-backups', type: 'contains', label: 'contains' },

		// MongoDB contains edges
		{ id: 'mongo-contains-store-products', source: 'mongo-store', target: 'mongo-store-products', type: 'contains', label: 'contains' },
		{ id: 'mongo-contains-store-reviews', source: 'mongo-store', target: 'mongo-store-reviews', type: 'contains', label: 'contains' },
		{ id: 'mongo-contains-store-categories', source: 'mongo-store', target: 'mongo-store-categories', type: 'contains', label: 'contains' },

		// S3 contains edges
		{ id: 's3-contains-assets-images', source: 's3-assets', target: 's3-assets-images/', type: 'contains', label: 'contains' },
		{ id: 's3-contains-assets-docs', source: 's3-assets', target: 's3-assets-documents/', type: 'contains', label: 'contains' },
		{ id: 's3-contains-backups-daily', source: 's3-backups', target: 's3-backups-daily/', type: 'contains', label: 'contains' },
		{ id: 's3-contains-backups-weekly', source: 's3-backups', target: 's3-backups-weekly/', type: 'contains', label: 'contains' },

		// PostgreSQL foreign keys
		{ id: 'fk-orders-user_id', source: 'pg-mydb-orders', target: 'pg-mydb-users', type: 'foreign_key', label: 'orders.user_id -> users.id' },
		{ id: 'fk-order_items-order_id', source: 'pg-mydb-order_items', target: 'pg-mydb-orders', type: 'foreign_key', label: 'order_items.order_id -> orders.id' },
		{ id: 'fk-order_items-product_id', source: 'pg-mydb-order_items', target: 'pg-mydb-products', type: 'foreign_key', label: 'order_items.product_id -> products.id' },
	],
};
