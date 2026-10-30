// Package redact masks credentials in data that leaves graph-go: node
// metadata, health metrics, error strings and logs end up in the UI, in
// `scan` output, in log collectors and in agents' context through MCP. It is
// a best-effort regex pass for the shapes graph-go actually produces (URLs
// with userinfo, DSN-style key=value pairs, env assignments, bearer tokens),
// not a general secret scanner.
package redact

import (
	"regexp"
	"strings"
)

// Mask replaces every redacted value.
const Mask = "***"

// secretNames are the credential words graph-go recognises, as a key on its
// own (password=…) or as the end of one (POSTGRES_PASSWORD=…).
const secretNames = `password|passwd|pwd|secret|secret_access_key|access_key_id|api[_-]?key|access[_-]?token|auth[_-]?token|token`

var (
	// scheme://user:password@host keeps the user, masks the password. The
	// user may be empty, as in redis://:password@host.
	urlUserinfo = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^/\s:@]*):[^/\s@]+@`)

	// key=value / key: value / "key": "value", where the key ends in a secret
	// name. A key only counts when a value follows, so a bare env var name
	// like POSTGRES_PASSWORD (e.g. in env_keys) is left alone. A quoted value
	// is one unit, spaces included.
	keyValue = regexp.MustCompile(`(?i)\b(\w*?(?:` + secretNames + `))("?\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s"'&;,]+)`)

	bearer = regexp.MustCompile(`(?i)\b(bearer)\s+[A-Za-z0-9._~+/=-]+`)

	// sensitiveKey matches a map key that names a credential: exactly a
	// secret name, or one after a _ - . separator (POSTGRES_PASSWORD,
	// db.password). Keys like env_keys or tokens do not match. DSN and URI
	// keys are not listed: String masks the credential inside them and keeps
	// the host.
	sensitiveKey = regexp.MustCompile(`(?i)(?:^|[_.-])(?:` + secretNames + `)$`)
)

// String masks credentials inside s.
func String(s string) string {
	s = urlUserinfo.ReplaceAllString(s, "${1}:"+Mask+"@")
	s = maskValues(s)
	return bearer.ReplaceAllString(s, "${1} "+Mask)
}

// maskValues replaces the value of every key=value match, keeping its quotes
// so the surrounding text (often JSON) keeps its shape.
func maskValues(s string) string {
	matches := keyValue.FindAllStringSubmatchIndex(s, -1)
	if matches == nil {
		return s
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		valStart, valEnd := m[6], m[7]
		b.WriteString(s[last:valStart])
		switch q := s[valStart]; q {
		case '"', '\'':
			b.WriteByte(q)
			b.WriteString(Mask)
			b.WriteByte(q)
		default:
			b.WriteString(Mask)
		}
		last = valEnd
	}
	b.WriteString(s[last:])
	return b.String()
}

// IsSensitiveKey reports whether a map key names a credential.
func IsSensitiveKey(k string) bool {
	return sensitiveKey.MatchString(k)
}

// Map returns a redacted deep copy of m; m itself is not modified, since
// adapters may hand out maps they keep using.
func Map(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if IsSensitiveKey(k) {
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
			if IsSensitiveKey(k) {
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
	case []map[string]any:
		out := make([]map[string]any, len(t))
		for i, e := range t {
			out[i] = Map(e)
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
