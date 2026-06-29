package docker

import (
	"testing"

	"github.com/guilherme-grimm/graph-go/internal/adapters"
)

func TestShouldIgnore(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   bool
	}{
		{"ignore true", map[string]string{"graphgo.ignore": "true"}, true},
		{"ignore True", map[string]string{"graphgo.ignore": "True"}, true},
		{"ignore TRUE", map[string]string{"graphgo.ignore": "TRUE"}, true},
		{"ignore false", map[string]string{"graphgo.ignore": "false"}, false},
		{"legacy ignore true", map[string]string{"graphinfo.ignore": "true"}, true},
		{"legacy ignore false", map[string]string{"graphinfo.ignore": "false"}, false},
		{"graphgo wins over legacy when both set", map[string]string{"graphgo.ignore": "false", "graphinfo.ignore": "true"}, false},
		{"no label", map[string]string{}, false},
		{"other labels", map[string]string{"com.docker.compose.service": "foo"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldIgnore(tt.labels)
			if got != tt.want {
				t.Errorf("ShouldIgnore(%v) = %v, want %v", tt.labels, got, tt.want)
			}
		})
	}
}

func TestApplyLabelOverrides_Type(t *testing.T) {
	labels := map[string]string{
		"graphgo.type": "postgres",
	}
	cfg := adapters.ConnectionConfig{"endpoint": "http://foo:8080"}

	resultType, _ := ApplyLabelOverrides(labels, TypeHTTP, cfg)
	if resultType != TypePostgres {
		t.Errorf("expected type override to postgres, got %s", resultType)
	}
}

func TestApplyLabelOverrides_DSN_Postgres(t *testing.T) {
	labels := map[string]string{
		"graphgo.dsn": "postgres://custom:pass@host:5432/db",
	}
	cfg := adapters.ConnectionConfig{"dsn": "original"}

	_, resultCfg := ApplyLabelOverrides(labels, TypePostgres, cfg)
	if resultCfg["dsn"] != "postgres://custom:pass@host:5432/db" {
		t.Errorf("expected DSN override, got %v", resultCfg["dsn"])
	}
}

func TestApplyLabelOverrides_DSN_MongoDB(t *testing.T) {
	labels := map[string]string{
		"graphgo.dsn": "mongodb://custom:pass@host:27017",
	}
	cfg := adapters.ConnectionConfig{"uri": "original"}

	_, resultCfg := ApplyLabelOverrides(labels, TypeMongoDB, cfg)
	if resultCfg["uri"] != "mongodb://custom:pass@host:27017" {
		t.Errorf("expected URI override, got %v", resultCfg["uri"])
	}
}

func TestApplyLabelOverrides_NodeType(t *testing.T) {
	labels := map[string]string{
		"graphgo.node-type": "gateway",
	}
	cfg := adapters.ConnectionConfig{}

	_, resultCfg := ApplyLabelOverrides(labels, TypeHTTP, cfg)
	if resultCfg["node_type"] != "gateway" {
		t.Errorf("expected node_type override, got %v", resultCfg["node_type"])
	}
}

func TestApplyLabelOverrides_Name(t *testing.T) {
	labels := map[string]string{
		"graphgo.name": "custom-name",
	}
	cfg := adapters.ConnectionConfig{}

	_, resultCfg := ApplyLabelOverrides(labels, TypeHTTP, cfg)
	if resultCfg["name"] != "custom-name" {
		t.Errorf("expected name override, got %v", resultCfg["name"])
	}
}

func TestApplyLabelOverrides_NoLabels(t *testing.T) {
	cfg := adapters.ConnectionConfig{"dsn": "original"}

	resultType, resultCfg := ApplyLabelOverrides(map[string]string{}, TypePostgres, cfg)
	if resultType != TypePostgres {
		t.Errorf("expected type unchanged, got %s", resultType)
	}
	if resultCfg["dsn"] != "original" {
		t.Errorf("expected config unchanged, got %v", resultCfg["dsn"])
	}
}

// TestApplyLabelOverrides_LegacyNamespace covers the deprecation window:
// graphinfo.* is still accepted, and graphgo.* wins when both are set.
func TestApplyLabelOverrides_LegacyNamespace(t *testing.T) {
	t.Run("legacy type override", func(t *testing.T) {
		labels := map[string]string{"graphinfo.type": "postgres"}
		resultType, _ := ApplyLabelOverrides(labels, TypeHTTP, adapters.ConnectionConfig{})
		if resultType != TypePostgres {
			t.Errorf("expected legacy label to override type, got %s", resultType)
		}
	})

	t.Run("legacy DSN override", func(t *testing.T) {
		labels := map[string]string{"graphinfo.dsn": "postgres://legacy:pass@host:5432/db"}
		_, resultCfg := ApplyLabelOverrides(labels, TypePostgres, adapters.ConnectionConfig{})
		if resultCfg["dsn"] != "postgres://legacy:pass@host:5432/db" {
			t.Errorf("expected legacy DSN override, got %v", resultCfg["dsn"])
		}
	})

	t.Run("legacy node-type override", func(t *testing.T) {
		labels := map[string]string{"graphinfo.node-type": "gateway"}
		_, resultCfg := ApplyLabelOverrides(labels, TypeHTTP, adapters.ConnectionConfig{})
		if resultCfg["node_type"] != "gateway" {
			t.Errorf("expected legacy node_type override, got %v", resultCfg["node_type"])
		}
	})

	t.Run("legacy name override", func(t *testing.T) {
		labels := map[string]string{"graphinfo.name": "legacy-name"}
		_, resultCfg := ApplyLabelOverrides(labels, TypeHTTP, adapters.ConnectionConfig{})
		if resultCfg["name"] != "legacy-name" {
			t.Errorf("expected legacy name override, got %v", resultCfg["name"])
		}
	})

	t.Run("graphgo takes precedence over legacy", func(t *testing.T) {
		labels := map[string]string{
			"graphgo.type":        "postgres",
			"graphinfo.type":      "mongodb",
			"graphgo.name":        "new-name",
			"graphinfo.name":      "old-name",
			"graphgo.node-type":   "gateway",
			"graphinfo.node-type": "api",
		}
		resultType, resultCfg := ApplyLabelOverrides(labels, TypeHTTP, adapters.ConnectionConfig{})
		if resultType != TypePostgres {
			t.Errorf("expected graphgo.type to win, got %s", resultType)
		}
		if resultCfg["name"] != "new-name" {
			t.Errorf("expected graphgo.name to win, got %v", resultCfg["name"])
		}
		if resultCfg["node_type"] != "gateway" {
			t.Errorf("expected graphgo.node-type to win, got %v", resultCfg["node_type"])
		}
	})
}
