package server

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/guilherme-grimm/graph-go/internal/adapters"
	"github.com/guilherme-grimm/graph-go/internal/graph"
)

// ScanGraph runs one discovery pass. When withHealth is true, one HealthAll
// sweep is performed and each metric's status is merged onto nodes via the
// Metadata["adapter"] lookup used by the websocket path.
func ScanGraph(reg adapters.Registry, withHealth bool) (*graph.Graph, error) {
	g, err := reg.DiscoverAll()
	if err != nil {
		return nil, fmt.Errorf("discover: %w", err)
	}

	if withHealth {
		metrics := reg.HealthAll()
		adapterHealth := make(map[string]string, len(metrics))
		for _, m := range metrics {
			adapterHealth[m.NodeID] = string(m.Status)
		}
		for i := range g.Nodes {
			adapterName, _ := g.Nodes[i].Metadata["adapter"].(string)
			if h, ok := adapterHealth[adapterName]; ok {
				g.Nodes[i].Health = h
			}
		}
	}

	return g, nil
}

// WriteGraphJSON runs ScanGraph and writes {"data": Graph} to w, matching
// the /api/graph HTTP envelope. When pretty is true, the output is indented
// with two spaces.
func WriteGraphJSON(w io.Writer, reg adapters.Registry, withHealth, pretty bool) error {
	g, err := ScanGraph(reg, withHealth)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(w)
	if pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(map[string]any{"data": g}); err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	return nil
}
