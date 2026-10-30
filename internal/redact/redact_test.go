package redact_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guilherme-grimm/graph-go/internal/redact"
)

func TestString(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"url userinfo", "http://admin:s3cret@api:8080/health", "http://admin:***@api:8080/health"},
		{"postgres url", "postgres://app:hunter2@db:5432/orders?sslmode=disable", "postgres://app:***@db:5432/orders?sslmode=disable"},
		{"mongodb+srv url", "mongodb+srv://u:p%40ss@cluster0.example.net/db", "mongodb+srv://u:***@cluster0.example.net/db"},
		{"url without password untouched", "redis://cache:6379/0", "redis://cache:6379/0"},
		{"url with user only untouched", "ssh://git@github.com/x", "ssh://git@github.com/x"},
		{"dsn key=value", "host=db user=app password=hunter2 dbname=orders", "host=db user=app password=*** dbname=orders"},
		{"mysql error", `access denied: user=root pwd=toor`, `access denied: user=root pwd=***`},
		{"json-ish", `{"secret": "abc123", "region": "us-east-1"}`, `{"secret": "***", "region": "us-east-1"}`},
		{"case insensitive", "PASSWORD=Xyz", "PASSWORD=***"},
		{"api key", "api_key: k-123 and apikey=k-456", "api_key: *** and apikey=***"},
		{"bearer token", "Authorization: Bearer eyJhbGciOi.abc.def", "Authorization: Bearer ***"},
		{"env var name alone untouched", "POSTGRES_PASSWORD", "POSTGRES_PASSWORD"},
		{"env var assignment masked", "POSTGRES_PASSWORD=hunter2", "POSTGRES_PASSWORD=***"},
		{"aws env assignment masked", "AWS_SECRET_ACCESS_KEY=abc/def", "AWS_SECRET_ACCESS_KEY=***"},
		{"github token env masked", "GITHUB_TOKEN: ghp_x1", "GITHUB_TOKEN: ***"},
		{"plain words untouched", "tokens: 5, passwords are hashed", "tokens: 5, passwords are hashed"},
		{"pgx connect error keeps user and db", "failed to connect to `user=postgres database=basics`: lookup db", "failed to connect to `user=postgres database=basics`: lookup db"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, redact.String(tt.in))
		})
	}
}

func TestMapMasksSensitiveKeysAndCopies(t *testing.T) {
	in := map[string]any{
		"endpoint": "http://u:p@api:8080",
		"Password": "hunter2",
		"labels":   map[string]string{"app": "api", "token": "t"},
		"nested":   map[string]any{"error": "password=x", "ports": []any{8080, "dsn=postgres://a:b@h/d"}},
		"env_keys": []string{"POSTGRES_PASSWORD", "AWS_SECRET_ACCESS_KEY"},
		"count":    3,
	}
	out := redact.Map(in)

	assert.Equal(t, "http://u:***@api:8080", out["endpoint"])
	assert.Equal(t, redact.Mask, out["Password"])
	assert.Equal(t, map[string]string{"app": "api", "token": redact.Mask}, out["labels"])
	nested := out["nested"].(map[string]any)
	assert.Equal(t, "password=***", nested["error"])
	assert.Equal(t, []any{8080, "dsn=postgres://a:***@h/d"}, nested["ports"])
	assert.Equal(t, []string{"POSTGRES_PASSWORD", "AWS_SECRET_ACCESS_KEY"}, out["env_keys"], "env var names are not secrets")
	assert.Equal(t, 3, out["count"])

	// The input is left as it was.
	assert.Equal(t, "hunter2", in["Password"])
	assert.Equal(t, "http://u:p@api:8080", in["endpoint"])
	require.Equal(t, "t", in["labels"].(map[string]string)["token"])
}

func TestMapNil(t *testing.T) {
	assert.Nil(t, redact.Map(nil))
}
