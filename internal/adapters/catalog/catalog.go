// Package catalog holds the list of adapters a graph-go binary ships with.
//
// This is the one file that answers "what infrastructure can graph-go probe?".
// It imports every adapter package; nothing imports it except composition roots
// (cmd/app). internal/server deliberately does not — it knows the Adapter
// interface and nothing about concrete adapters. See ADR-0002.
//
// The list is compile-time. It is never derived from user config: the moment a
// user has to declare which adapters exist, "Zero Config" stops being true.
package catalog

import (
	"go.uber.org/zap"

	"github.com/guilherme-grimm/graph-go/internal/adapters"
	"github.com/guilherme-grimm/graph-go/internal/adapters/elasticsearch"
	"github.com/guilherme-grimm/graph-go/internal/adapters/http"
	"github.com/guilherme-grimm/graph-go/internal/adapters/mongodb"
	"github.com/guilherme-grimm/graph-go/internal/adapters/mysql"
	"github.com/guilherme-grimm/graph-go/internal/adapters/postgres"
	"github.com/guilherme-grimm/graph-go/internal/adapters/redis"
	"github.com/guilherme-grimm/graph-go/internal/adapters/s3"
)

// Default returns the Catalog of every adapter this binary supports. The keys
// are connection types: what Docker classification and the YAML `type:` field
// both produce.
//
// Adding an adapter means adding one line here. The wrapper closures are
// required because each package's New returns an unexported concrete type,
// which Go will not implicitly convert to adapters.Adapter.
func Default() adapters.Catalog {
	return adapters.NewCatalog(map[string]adapters.AdapterConstructor{
		"postgres":      func(l *zap.SugaredLogger) adapters.Adapter { return postgres.New(l) },
		"mysql":         func(l *zap.SugaredLogger) adapters.Adapter { return mysql.New(l) },
		"mongodb":       func(l *zap.SugaredLogger) adapters.Adapter { return mongodb.New(l) },
		"redis":         func(l *zap.SugaredLogger) adapters.Adapter { return redis.New(l) },
		"elasticsearch": func(l *zap.SugaredLogger) adapters.Adapter { return elasticsearch.New(l) },
		"s3":            func(l *zap.SugaredLogger) adapters.Adapter { return s3.New(l) },
		"http":          func(l *zap.SugaredLogger) adapters.Adapter { return http.New(l) },
	})
}
