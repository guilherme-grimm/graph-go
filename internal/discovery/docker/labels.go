package docker

import (
	"strings"

	"github.com/guilherme-grimm/graph-go/internal/adapters"
)

const (
	LabelIgnore   = "graphgo.ignore"
	LabelType     = "graphgo.type"
	LabelDSN      = "graphgo.dsn"
	LabelNodeType = "graphgo.node-type"
	LabelName     = "graphgo.name"

	// Legacy* labels carry graph-go's pre-rename namespace (graphinfo.*).
	// Accepted during the deprecation window so existing labelled containers
	// keep working. graphgo.* wins when a container sets both.
	LegacyLabelIgnore   = "graphinfo.ignore"
	LegacyLabelType     = "graphinfo.type"
	LegacyLabelDSN      = "graphinfo.dsn"
	LegacyLabelNodeType = "graphinfo.node-type"
	LegacyLabelName     = "graphinfo.name"
)

// labelValue resolves a logical label, preferring the graphgo.* namespace and
// falling back to the legacy graphinfo.* namespace when the new key is absent.
// Returns "", false when neither namespace sets the label.
func labelValue(labels map[string]string, key, legacyKey string) (string, bool) {
	if v, ok := labels[key]; ok {
		return v, true
	}
	if v, ok := labels[legacyKey]; ok {
		return v, true
	}
	return "", false
}

// ShouldIgnore returns true if the container has graphgo.ignore=true (or the
// legacy graphinfo.ignore=true).
func ShouldIgnore(labels map[string]string) bool {
	v, ok := labelValue(labels, LabelIgnore, LegacyLabelIgnore)
	return ok && strings.EqualFold(v, "true")
}

// ApplyLabelOverrides applies graphgo.* (and legacy graphinfo.*) label
// overrides to the detected service type and connection config. graphgo.*
// takes precedence when both namespaces are present on a container. Returns
// the (possibly modified) type and config.
func ApplyLabelOverrides(labels map[string]string, detectedType ServiceType, config adapters.ConnectionConfig) (ServiceType, adapters.ConnectionConfig) {
	resultType := detectedType

	if v, ok := labelValue(labels, LabelType, LegacyLabelType); ok && v != "" {
		resultType = ServiceType(v)
	}

	if v, ok := labelValue(labels, LabelDSN, LegacyLabelDSN); ok && v != "" {
		switch resultType {
		case TypePostgres:
			config["dsn"] = v
		case TypeMongoDB:
			config["uri"] = v
		default:
			config["dsn"] = v
		}
	}

	if v, ok := labelValue(labels, LabelNodeType, LegacyLabelNodeType); ok && v != "" {
		config["node_type"] = v
	}

	if v, ok := labelValue(labels, LabelName, LegacyLabelName); ok && v != "" {
		config["name"] = v
	}

	return resultType, config
}
