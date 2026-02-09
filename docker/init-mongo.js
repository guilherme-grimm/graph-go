// Seed data for graph-info development environment.
// MongoDB executes /docker-entrypoint-initdb.d/*.js against the MONGO_INITDB_DATABASE.

// Switch to 'store' database
db = db.getSiblingDB('store');

// Create collections with sample documents
db.createCollection('products');
db.products.insertMany([
  { name: 'Widget', price: 9.99, category: 'tools', inStock: true },
  { name: 'Gadget', price: 24.99, category: 'electronics', inStock: true },
  { name: 'Gizmo', price: 14.50, category: 'tools', inStock: false },
]);

db.createCollection('reviews');
db.reviews.insertMany([
  { productName: 'Widget', rating: 5, comment: 'Great product!', author: 'alice' },
  { productName: 'Gadget', rating: 4, comment: 'Pretty good', author: 'bob' },
]);

db.createCollection('categories');
db.categories.insertMany([
  { name: 'tools', description: 'Hand and power tools' },
  { name: 'electronics', description: 'Electronic devices and accessories' },
]);
