package docker

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/guilherme-grimm/graph-go/internal/adapters"
)

// ExtractCredentials builds an adapters.ConnectionConfig from container
// environment variables, based on the detected service type, reaching the
// container at containerHost on the service's default port.
func ExtractCredentials(svcType ServiceType, envVars []string, containerHost string, containerName string) adapters.ConnectionConfig {
	return ExtractCredentialsAt(svcType, envVars, containerHost, servicePort(svcType, parseEnvVars(envVars)), containerName)
}

// ExtractCredentialsAt is ExtractCredentials with an explicit host and port,
// for when the service is reached somewhere other than its container port
// (a published host port, for example).
func ExtractCredentialsAt(svcType ServiceType, envVars []string, host string, port uint16, containerName string) adapters.ConnectionConfig {
	env := parseEnvVars(envVars)

	switch svcType {
	case TypePostgres:
		return extractPostgresCredentials(env, host, port)
	case TypeMongoDB:
		return extractMongoCredentials(env, host, port)
	case TypeS3:
		return extractS3Credentials(env, host, port)
	case TypeRedis:
		return extractRedisCredentials(env, host, port)
	case TypeMySQL:
		return extractMySQLCredentials(env, host, port)
	case TypeElasticsearch:
		return extractElasticsearchCredentials(env, host, port)
	case TypeHTTP:
		return extractHTTPCredentials(host, port, containerName)
	default:
		return adapters.ConnectionConfig{}
	}
}

// servicePort is the port the service listens on inside its container: the
// type's default, or $PORT for HTTP services.
func servicePort(svcType ServiceType, env map[string]string) uint16 {
	if svcType == TypeHTTP {
		if p, err := strconv.ParseUint(env["PORT"], 10, 16); err == nil && p > 0 {
			return uint16(p)
		}
		return 8080
	}
	return DefaultPortForType(svcType)
}

func parseEnvVars(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, e := range env {
		k, v, ok := strings.Cut(e, "=")
		if ok {
			m[k] = v
		}
	}
	return m
}

func extractPostgresCredentials(env map[string]string, host string, port uint16) adapters.ConnectionConfig {
	user := envOrDefault(env, "POSTGRES_USER", "postgres")
	pass := envOrDefault(env, "POSTGRES_PASSWORD", "")
	db := envOrDefault(env, "POSTGRES_DB", user)

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		url.QueryEscape(user), url.QueryEscape(pass), host, port, db)

	return adapters.ConnectionConfig{"dsn": dsn}
}

func extractMongoCredentials(env map[string]string, host string, port uint16) adapters.ConnectionConfig {
	user := envOrDefault(env, "MONGO_INITDB_ROOT_USERNAME", "")
	pass := envOrDefault(env, "MONGO_INITDB_ROOT_PASSWORD", "")

	var uri string
	if user != "" && pass != "" {
		uri = fmt.Sprintf("mongodb://%s:%s@%s:%d",
			url.QueryEscape(user), url.QueryEscape(pass), host, port)
	} else {
		uri = fmt.Sprintf("mongodb://%s:%d",
			host, port)
	}

	return adapters.ConnectionConfig{"uri": uri}
}

func extractS3Credentials(env map[string]string, host string, port uint16) adapters.ConnectionConfig {
	accessKey := envOrDefault(env, "MINIO_ROOT_USER", "minioadmin")
	secretKey := envOrDefault(env, "MINIO_ROOT_PASSWORD", "minioadmin")
	if isRustFS(env) {
		// RustFS also honors MINIO_ROOT_*, but its own keys win and its
		// built-in default differs.
		accessKey = envOrDefault(env, "RUSTFS_ACCESS_KEY", envOrDefault(env, "MINIO_ROOT_USER", "rustfsadmin"))
		secretKey = envOrDefault(env, "RUSTFS_SECRET_KEY", envOrDefault(env, "MINIO_ROOT_PASSWORD", "rustfsadmin"))
	}
	endpoint := fmt.Sprintf("http://%s:%d", host, port)

	return adapters.ConnectionConfig{
		"endpoint":          endpoint,
		"access_key_id":     accessKey,
		"secret_access_key": secretKey,
		"region":            "us-east-1",
	}
}

func extractRedisCredentials(env map[string]string, host string, port uint16) adapters.ConnectionConfig {
	pass := envOrDefault(env, "REDIS_PASSWORD", "")

	cfg := adapters.ConnectionConfig{
		"host": host,
		"port": port,
	}
	if pass != "" {
		cfg["password"] = pass
	}
	return cfg
}

func extractHTTPCredentials(host string, port uint16, containerName string) adapters.ConnectionConfig {
	endpoint := fmt.Sprintf("http://%s:%d", host, port)

	return adapters.ConnectionConfig{
		"endpoint":    endpoint,
		"health_path": "/health",
		"name":        containerName,
	}
}

func extractMySQLCredentials(env map[string]string, host string, port uint16) adapters.ConnectionConfig {
	user := envOrDefault(env, "MYSQL_USER", "root")
	pass := envOrDefault(env, "MYSQL_ROOT_PASSWORD", "")
	if pass == "" {
		pass = envOrDefault(env, "MYSQL_PASSWORD", "")
	}
	db := envOrDefault(env, "MYSQL_DATABASE", "")

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s",
		user, url.QueryEscape(pass), host, port, db)

	return adapters.ConnectionConfig{"dsn": dsn}
}

func extractElasticsearchCredentials(env map[string]string, host string, port uint16) adapters.ConnectionConfig {
	endpoint := fmt.Sprintf("http://%s:%d", host, port)

	cfg := adapters.ConnectionConfig{
		"endpoint": endpoint,
	}

	user := envOrDefault(env, "ELASTIC_USERNAME", "elastic")
	pass := envOrDefault(env, "ELASTIC_PASSWORD", "")
	if pass != "" {
		cfg["username"] = user
		cfg["password"] = pass
	}

	return cfg
}

// isRustFS reports whether a container is RustFS. The official image sets
// RUSTFS_* variables in its own ENV, so they show up even when the user
// configures nothing.
func isRustFS(env map[string]string) bool {
	for k := range env {
		if strings.HasPrefix(k, "RUSTFS_") {
			return true
		}
	}
	return false
}

func envOrDefault(env map[string]string, key, fallback string) string {
	if v, ok := env[key]; ok && v != "" {
		return v
	}
	return fallback
}
