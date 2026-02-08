package config

import (
	"fmt"

	"binary/internal/adapters"
	"binary/internal/adapters/mongodb"
	"binary/internal/adapters/postgres"
	"binary/internal/adapters/s3"
)

func AdapterFactory(connType string) (adapters.Adapter, error) {
	switch connType {
	case "postgres":
		return postgres.New(), nil
	case "mongodb":
		return mongodb.New(), nil
	case "s3":
		return s3.New(), nil
	default:
		return nil, fmt.Errorf("config: unknown adapter type %q", connType)
	}
}
