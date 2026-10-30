// Package redact masks credentials in data that leaves graph-go: node
// metadata, health metrics and error strings end up in the UI, in `scan`
// output and in agents' context through MCP. It is a best-effort regex pass
// for the shapes graph-go actually produces (URLs with userinfo, DSN-style
// key=value pairs, bearer tokens), not a general secret scanner.
package redact

import (
	"regexp"
	"strings"
)

// Mask replaces every redacted value.
const Mask = "***"

var (
	// scheme://user:password@host keeps the user, masks the password.
	urlUserinfo = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^/\s:@]+):[^/\s@]+@`)

	// password=x, "secret": "x", POSTGRES_PASSWORD=x, api_key: x … as found in
	// DSNs, env assignments, errors and JSON-ish strings. A key only counts
	// when a value follows, so a bare env var name like POSTGRES_PASSWORD
	// (e.g. in env_keys) is left alone.
	keyValue = regexp.MustCompile(`(?i)\b(\w*?(?:password|passwd|pwd|secret|secret_access_key|access_key_id|api[_-]?key|access[_-]?token|auth[_-]?token|token))("?\s*[=:]\s*"?)([^\s"'&;,]+)`)

	bearer = regexp.MustCompile(`(?i)\b(bearer)\s+[A-Za-z0-9._~+/=-]+`)
)

// sensitiveKeys are map keys whose value is masked whole. DSNs and URIs are
// not listed: String masks the credential inside them and keeps the host.
var sensitiveKeys = map[string]bool{
	"password": true, "passwd": true, "pwd": true, "secret": true,
	"secret_access_key": true, "access_key_id": true, "api_key": true, "apikey": true,
	"token": true, "access_token": true, "auth_token": true,
}

// String masks credentials inside s.
func String(s string) string {
	s = urlUserinfo.ReplaceAllString(s, "${1}:"+Mask+"@")
	s = keyValue.ReplaceAllString(s, "${1}${2}"+Mask)
	return bearer.ReplaceAllString(s, "${1} "+Mask)
}

// Map returns a redacted deep copy of m; m itself is not modified, since
// adapters may hand out maps they keep using.
func Map(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if sensitiveKeys[strings.ToLower(k)] {
			out[k] = Mask
			continue
		}
		out[k] = Value(v)
	}
	return out
}

// Value redacts strings anywhere inside v, copying maps and slices.
func Value(v any) any {
	switch t := v.(type) {
	case string:
		return String(t)
	case map[string]any:
		return Map(t)
	case map[string]string:
		out := make(map[string]string, len(t))
		for k, s := range t {
			if sensitiveKeys[strings.ToLower(k)] {
				out[k] = Mask
				continue
			}
			out[k] = String(s)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = Value(e)
		}
		return out
	case []string:
		out := make([]string, len(t))
		for i, s := range t {
			out[i] = String(s)
		}
		return out
	case []map[string]string:
		out := make([]map[string]string, len(t))
		for i, e := range t {
			out[i] = Value(e).(map[string]string)
		}
		return out
	default:
		return v
	}
}
