package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"

	"github.com/guilherme-grimm/graph-go/internal/adapters"

	"gopkg.in/yaml.v3"
)

type DockerConfig struct {
	Enabled      *bool    `yaml:"enabled,omitempty"` // nil = auto-detect
	Socket       string   `yaml:"socket,omitempty"`  // default: /var/run/docker.sock
	Network      string   `yaml:"network,omitempty"` // limit to specific network
	IgnoreImages []string `yaml:"ignore_images,omitempty"`
}

type KubernetesConfig struct {
	Enabled    *bool    `yaml:"enabled,omitempty"`    // nil = auto-detect
	Kubeconfig string   `yaml:"kubeconfig,omitempty"` // path override; empty = default lookup
	Context    string   `yaml:"context,omitempty"`    // empty = current context
	Namespaces []string `yaml:"namespaces,omitempty"` // empty = all namespaces
}

type ServerConfig struct {
	Port           int      `yaml:"port,omitempty"`
	AllowedOrigins []string `yaml:"allowed_origins,omitempty"`
}

type Config struct {
	Server      ServerConfig      `yaml:"server,omitempty"`
	Docker      DockerConfig      `yaml:"docker,omitempty"`
	Kubernetes  KubernetesConfig  `yaml:"kubernetes,omitempty"`
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
	Username        string           `yaml:"username,omitempty"`
	Password        string           `yaml:"password,omitempty"`
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

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Check reads the config at path and reports every problem it finds instead
// of stopping at the first one. Unlike Load it rejects unknown keys, so a
// misspelled or unsupported field is reported rather than silently dropped.
// The returned error is reserved for failures to read the file at all.
func Check(path string) ([]error, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: failed to read %s: %w", path, err)
	}

	var problems []error

	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		var typeErr *yaml.TypeError
		if !errors.As(err, &typeErr) {
			// Syntax errors leave nothing reliable to validate.
			return []error{fmt.Errorf("config: failed to parse %s: %w", path, err)}, nil
		}
		// Type errors still decode the rest of the document, so keep going.
		for _, msg := range typeErr.Errors {
			problems = append(problems, fmt.Errorf("config: %s", friendlyTypeError(msg)))
		}
	}

	// serve reads only the first document, so anything after a "---" is
	// silently ignored there; say so here.
	// Empty documents (a trailing "---") are harmless and skipped.
	for {
		var extra yaml.Node
		if err := dec.Decode(&extra); err != nil {
			break
		}
		if hasContent(&extra) {
			problems = append(problems, errors.New("config: only one YAML document is supported; everything after the first '---' separator is ignored"))
			break
		}
	}

	return append(problems, cfg.Problems()...), nil
}

// hasContent reports whether a decoded document holds anything but null.
func hasContent(doc *yaml.Node) bool {
	for _, n := range doc.Content {
		if n.Kind != yaml.ScalarNode || n.Tag != "!!null" {
			return true
		}
	}
	return false
}

// unknownFieldRe matches yaml.v3's KnownFields message, which names Go types.
var unknownFieldRe = regexp.MustCompile(`^(line \d+): field (\S+) not found in type \S+$`)

func friendlyTypeError(msg string) string {
	if m := unknownFieldRe.FindStringSubmatch(msg); m != nil {
		return fmt.Sprintf("%s: unknown key '%s'", m[1], m[2])
	}
	return msg
}

// Validate returns every problem in cfg joined into one error, or nil.
func (cfg *Config) Validate() error {
	return errors.Join(cfg.Problems()...)
}

// Problems lists every missing required field and unknown connection type.
func (cfg *Config) Problems() []error {
	var problems []error
	for i, conn := range cfg.Connections {
		if conn.Name == "" {
			problems = append(problems, fmt.Errorf("config: connections[%d]: 'name' is required", i))
		}
		if conn.Type == "" {
			problems = append(problems, fmt.Errorf("config: connections[%d] (%s): 'type' is required", i, conn.Name))
			continue
		}
		if msg := conn.missingField(); msg != "" {
			problems = append(problems, fmt.Errorf("config: connections[%d] (%s): %s", i, conn.Name, msg))
		}
	}
	return problems
}

// missingField returns why e lacks a field its type requires, or "".
func (e *ConnectionEntry) missingField() string {
	switch e.Type {
	case "postgres":
		if e.DSN == "" {
			return "'dsn' is required for postgres"
		}
	case "mongodb":
		if e.URI == "" {
			return "'uri' is required for mongodb"
		}
	case "s3":
		if e.Endpoint == "" && e.Region == "" {
			return "'endpoint' or 'region' is required for s3"
		}
	case "http":
		if e.Endpoint == "" {
			return "'endpoint' is required for http"
		}
	case "mysql":
		if e.DSN == "" {
			return "'dsn' is required for mysql"
		}
	case "elasticsearch":
		if e.Endpoint == "" {
			return "'endpoint' is required for elasticsearch"
		}
	case "redis":
		if e.URI == "" && e.Endpoint == "" {
			return "'uri' or 'endpoint' is required for redis"
		}
	default:
		return fmt.Sprintf("unknown type %q", e.Type)
	}
	return ""
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
	case "mysql":
		if e.DSN != "" {
			cfg["dsn"] = e.DSN
		}
	case "redis":
		if e.URI != "" {
			cfg["uri"] = e.URI
		}
		if e.Endpoint != "" {
			cfg["host"] = e.Endpoint
		}
		if e.Password != "" {
			cfg["password"] = e.Password
		}
	case "elasticsearch":
		if e.Endpoint != "" {
			cfg["endpoint"] = e.Endpoint
		}
		if e.Username != "" {
			cfg["username"] = e.Username
		}
		if e.Password != "" {
			cfg["password"] = e.Password
		}
	default:
		// Unknown types fall through; the Catalog lookup surfaces the error at the call site.
	}

	return cfg
}
