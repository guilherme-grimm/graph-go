package adapters

import "github.com/google/uuid"

type ConnectionConfig map[string]any
type HealthMetrics map[string]any

// Contracts that bind the adapters and what they should do
type Adapter interface {
	// Simple connection to the service, fetches from the config file
	Connect(config ConnectionConfig) error

	// Recursive method using BFS  to find and map everything
	Discover() ([]Node, []Edge, error)

	// Just a abstract method of how to get basic health metrics
	Health() (HealthMetrics, error)

	// Entrypoint for connection closing upon shutting down the service
	Close() error
}

// Core struct, the nodes of the service
type Node struct {
	Id       uuid.UUID
	Type     string
	Name     string
	Parent   string
	Metadata map[string]any
}

// Connection and boundary data
type Edge struct {
	Id     string
	Source uuid.UUID
	Target uuid.UUID
	Type   string
	Label  string
}
