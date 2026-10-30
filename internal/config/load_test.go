package config_test

import (
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/guilherme-grimm/graph-go/internal/config"
)

// serve and scan fall back to auto-discovery quietly when the config file is
// missing, which relies on the not-exist error surviving Load's wrapping.
func TestLoadMissingFileIsNotExist(t *testing.T) {
	_, err := config.Load(filepath.Join(t.TempDir(), "config.yaml"))
	require.ErrorIs(t, err, fs.ErrNotExist)
}
