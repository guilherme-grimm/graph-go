package docker

import (
	"strings"
	"testing"
)

func TestExtractCredentials_Postgres(t *testing.T) {
	env := []string{
		"POSTGRES_USER=myuser",
		"POSTGRES_PASSWORD=mypass",
		"POSTGRES_DB=mydb",
	}

	cfg := ExtractCredentials(TypePostgres, env, "postgres-host", "postgres")

	dsn, ok := cfg["dsn"].(string)
	if !ok {
		t.Fatal("expected dsn string in config")
	}
	if !strings.Contains(dsn, "myuser:mypass@postgres-host:5432/mydb") {
		t.Errorf("unexpected DSN: %s", dsn)
	}
}

func TestExtractCredentials_PostgresDefaults(t *testing.T) {
	cfg := ExtractCredentials(TypePostgres, nil, "pg", "postgres")

	dsn := cfg["dsn"].(string)
	if !strings.Contains(dsn, "postgres:@pg:5432/postgres") {
		t.Errorf("expected default user/db in DSN, got: %s", dsn)
	}
}

func TestExtractCredentials_MongoDB(t *testing.T) {
	env := []string{
		"MONGO_INITDB_ROOT_USERNAME=admin",
		"MONGO_INITDB_ROOT_PASSWORD=secret",
	}

	cfg := ExtractCredentials(TypeMongoDB, env, "mongo-host", "mongo")

	uri, ok := cfg["uri"].(string)
	if !ok {
		t.Fatal("expected uri string in config")
	}
	if !strings.Contains(uri, "admin:secret@mongo-host:27017") {
		t.Errorf("unexpected URI: %s", uri)
	}
}

func TestExtractCredentials_MongoDBNoAuth(t *testing.T) {
	cfg := ExtractCredentials(TypeMongoDB, nil, "mongo-host", "mongo")

	uri := cfg["uri"].(string)
	if !strings.Contains(uri, "mongodb://mongo-host:27017") {
		t.Errorf("expected no-auth URI, got: %s", uri)
	}
	if strings.Contains(uri, "@") {
		t.Errorf("expected no @ in no-auth URI, got: %s", uri)
	}
}

func TestExtractCredentials_S3(t *testing.T) {
	env := []string{
		"MINIO_ROOT_USER=myadmin",
		"MINIO_ROOT_PASSWORD=myadminpass",
	}

	cfg := ExtractCredentials(TypeS3, env, "minio-host", "minio")

	if cfg["access_key_id"] != "myadmin" {
		t.Errorf("unexpected access_key_id: %v", cfg["access_key_id"])
	}
	if cfg["secret_access_key"] != "myadminpass" {
		t.Errorf("unexpected secret_access_key: %v", cfg["secret_access_key"])
	}
	endpoint := cfg["endpoint"].(string)
	if !strings.Contains(endpoint, "minio-host:9000") {
		t.Errorf("unexpected endpoint: %s", endpoint)
	}
}

func TestExtractCredentials_S3RustFS(t *testing.T) {
	// RUSTFS_VOLUMES and friends come from the official image's own ENV.
	image := []string{"RUSTFS_VOLUMES=/data", "RUSTFS_OBS_LOGGER_LEVEL=warn"}

	tests := []struct {
		name       string
		env        []string
		wantAccess string
		wantSecret string
	}{
		{name: "rustfs keys", env: append([]string{"RUSTFS_ACCESS_KEY=ak", "RUSTFS_SECRET_KEY=sk"}, image...), wantAccess: "ak", wantSecret: "sk"},
		{name: "minio compat keys", env: append([]string{"MINIO_ROOT_USER=mu", "MINIO_ROOT_PASSWORD=mp"}, image...), wantAccess: "mu", wantSecret: "mp"},
		{name: "rustfs keys win over compat", env: append([]string{"RUSTFS_ACCESS_KEY=ak", "MINIO_ROOT_USER=mu", "RUSTFS_SECRET_KEY=sk", "MINIO_ROOT_PASSWORD=mp"}, image...), wantAccess: "ak", wantSecret: "sk"},
		{name: "rustfs defaults", env: image, wantAccess: "rustfsadmin", wantSecret: "rustfsadmin"},
		{name: "plain minio defaults unchanged", env: nil, wantAccess: "minioadmin", wantSecret: "minioadmin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ExtractCredentials(TypeS3, tt.env, "s3-host", "s3")
			if cfg["access_key_id"] != tt.wantAccess {
				t.Errorf("access_key_id = %v, want %s", cfg["access_key_id"], tt.wantAccess)
			}
			if cfg["secret_access_key"] != tt.wantSecret {
				t.Errorf("secret_access_key = %v, want %s", cfg["secret_access_key"], tt.wantSecret)
			}
		})
	}
}

func TestExtractCredentials_Redis(t *testing.T) {
	env := []string{"REDIS_PASSWORD=redispass"}
	cfg := ExtractCredentials(TypeRedis, env, "redis-host", "redis")

	if cfg["host"] != "redis-host" {
		t.Errorf("unexpected host: %v", cfg["host"])
	}
	if cfg["password"] != "redispass" {
		t.Errorf("unexpected password: %v", cfg["password"])
	}
}

func TestExtractCredentials_MySQL(t *testing.T) {
	env := []string{
		"MYSQL_ROOT_PASSWORD=rootpass",
		"MYSQL_USER=myuser",
		"MYSQL_DATABASE=mydb",
	}

	cfg := ExtractCredentials(TypeMySQL, env, "mysql-host", "mysql")

	dsn, ok := cfg["dsn"].(string)
	if !ok {
		t.Fatal("expected dsn string in config")
	}
	if !strings.Contains(dsn, "myuser:rootpass@tcp(mysql-host:3306)/mydb") {
		t.Errorf("unexpected DSN: %s", dsn)
	}
}

func TestExtractCredentials_MySQLDefaults(t *testing.T) {
	cfg := ExtractCredentials(TypeMySQL, nil, "mysql-host", "mysql")

	dsn := cfg["dsn"].(string)
	if !strings.Contains(dsn, "root:@tcp(mysql-host:3306)/") {
		t.Errorf("expected default user in DSN, got: %s", dsn)
	}
}

func TestExtractCredentials_MySQLFallbackPassword(t *testing.T) {
	env := []string{
		"MYSQL_PASSWORD=userpass",
		"MYSQL_DATABASE=mydb",
	}

	cfg := ExtractCredentials(TypeMySQL, env, "mysql-host", "mysql")

	dsn := cfg["dsn"].(string)
	if !strings.Contains(dsn, "root:userpass@tcp(mysql-host:3306)/mydb") {
		t.Errorf("expected MYSQL_PASSWORD fallback, got: %s", dsn)
	}
}

func TestExtractCredentials_Elasticsearch(t *testing.T) {
	env := []string{
		"ELASTIC_USERNAME=elastic",
		"ELASTIC_PASSWORD=changeme",
	}

	cfg := ExtractCredentials(TypeElasticsearch, env, "es-host", "elasticsearch")

	endpoint := cfg["endpoint"].(string)
	if !strings.Contains(endpoint, "es-host:9200") {
		t.Errorf("unexpected endpoint: %s", endpoint)
	}
	if cfg["username"] != "elastic" {
		t.Errorf("unexpected username: %v", cfg["username"])
	}
	if cfg["password"] != "changeme" {
		t.Errorf("unexpected password: %v", cfg["password"])
	}
}

func TestExtractCredentials_ElasticsearchNoAuth(t *testing.T) {
	cfg := ExtractCredentials(TypeElasticsearch, nil, "es-host", "elasticsearch")

	endpoint := cfg["endpoint"].(string)
	if !strings.Contains(endpoint, "es-host:9200") {
		t.Errorf("unexpected endpoint: %s", endpoint)
	}
	if _, ok := cfg["username"]; ok {
		t.Error("expected no username when no password is set")
	}
}

func TestExtractCredentials_HTTP(t *testing.T) {
	env := []string{"PORT=3000"}
	cfg := ExtractCredentials(TypeHTTP, env, "myapp", "my-service")

	endpoint := cfg["endpoint"].(string)
	if !strings.Contains(endpoint, "myapp:3000") {
		t.Errorf("unexpected endpoint: %s", endpoint)
	}
	if cfg["name"] != "my-service" {
		t.Errorf("unexpected name: %v", cfg["name"])
	}
}

func TestExtractCredentials_HTTPDefaultPort(t *testing.T) {
	cfg := ExtractCredentials(TypeHTTP, nil, "myapp", "my-service")

	endpoint := cfg["endpoint"].(string)
	if !strings.Contains(endpoint, "myapp:8080") {
		t.Errorf("expected default port 8080, got: %s", endpoint)
	}
}

func TestParseEnvVars(t *testing.T) {
	env := []string{
		"KEY1=value1",
		"KEY2=value2=with=equals",
		"EMPTY=",
		"NOEQUALS",
	}

	m := parseEnvVars(env)

	if m["KEY1"] != "value1" {
		t.Errorf("KEY1 = %q, want %q", m["KEY1"], "value1")
	}
	if m["KEY2"] != "value2=with=equals" {
		t.Errorf("KEY2 = %q, want %q", m["KEY2"], "value2=with=equals")
	}
	if m["EMPTY"] != "" {
		t.Errorf("EMPTY = %q, want empty", m["EMPTY"])
	}
	if _, ok := m["NOEQUALS"]; ok {
		t.Error("NOEQUALS should not be in map")
	}
}
