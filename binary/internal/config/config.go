package config

import (
	"fmt"
	"os"

	"binary/internal/adapters"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Connections []ConnectionEntry `yaml:"connections"`
}

type DependsOnEntry struct {
	Target string `yaml:"target"`
	Label  string `yaml:"label,omitempty"`
}

type ConnectionEntry struct {
	Name            string           `yaml:"name"`
	Type            string           `yaml:"type"`
	DSN             string           `yaml:"dsn,omitempty"`
	URI             string           `yaml:"uri,omitempty"`
	Region          string           `yaml:"region,omitempty"`
	Endpoint        string           `yaml:"endpoint,omitempty"`
	AccessKeyID     string           `yaml:"access_key_id,omitempty"`
	SecretAccessKey string           `yaml:"secret_access_key,omitempty"`
	HealthPath      string           `yaml:"health_path,omitempty"`
	NodeType        string           `yaml:"node_type,omitempty"`
	DependsOn       []DependsOnEntry `yaml:"depends_on,omitempty"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: failed to read %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: failed to parse %s: %w", path, err)
	}

	return &cfg, nil
}

func (e *ConnectionEntry) ToConnectionConfig() adapters.ConnectionConfig {
	cfg := adapters.ConnectionConfig{}

	switch e.Type {
	case "postgres":
		if e.DSN != "" {
			cfg["dsn"] = e.DSN
		}
	case "mongodb":
		if e.URI != "" {
			cfg["uri"] = e.URI
		}
	case "s3":
		if e.Region != "" {
			cfg["region"] = e.Region
		}
		if e.Endpoint != "" {
			cfg["endpoint"] = e.Endpoint
		}
		if e.AccessKeyID != "" {
			cfg["access_key_id"] = e.AccessKeyID
		}
		if e.SecretAccessKey != "" {
			cfg["secret_access_key"] = e.SecretAccessKey
		}
	case "http":
		if e.Endpoint != "" {
			cfg["endpoint"] = e.Endpoint
		}
		if e.HealthPath != "" {
			cfg["health_path"] = e.HealthPath
		}
		if e.NodeType != "" {
			cfg["node_type"] = e.NodeType
		}
		cfg["name"] = e.Name
		if len(e.DependsOn) > 0 {
			deps := make([]map[string]string, len(e.DependsOn))
			for i, d := range e.DependsOn {
				deps[i] = map[string]string{"target": d.Target, "label": d.Label}
			}
			cfg["depends_on"] = deps
		}
	}

	return cfg
}
