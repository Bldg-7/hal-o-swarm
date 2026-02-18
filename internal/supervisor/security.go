package supervisor

import (
	"crypto/tls"
	"encoding/json"
	"strings"

	"github.com/Bldg-7/hal-o-swarm/internal/config"
)

func LoadTLSConfig(cfg config.TLSConfig) (*tls.Config, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	cert, err := tls.LoadX509KeyPair(cfg.CertPath, cfg.KeyPath)
	if err != nil {
		return nil, err
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

func MatchOrigin(origin string, pattern string) bool {
	if pattern == "*" {
		return true
	}

	if strings.Contains(pattern, "*") {
		return matchOriginWildcard(origin, pattern)
	}

	return origin == pattern
}

func matchOriginWildcard(origin, pattern string) bool {
	if strings.HasSuffix(pattern, ":*") {
		prefix := strings.TrimSuffix(pattern, ":*")
		originNoPort := origin
		if idx := strings.LastIndex(origin, ":"); idx > strings.Index(origin, "//") {
			originNoPort = origin[:idx]
		}
		return originNoPort == prefix
	}

	if strings.HasPrefix(pattern, "https://*.") {
		suffix := strings.TrimPrefix(pattern, "https://*")
		if !strings.HasPrefix(origin, "https://") {
			return false
		}
		host := strings.TrimPrefix(origin, "https://")
		return strings.HasSuffix(host, suffix) && !strings.HasPrefix(host, "*")
	}

	if strings.HasPrefix(pattern, "http://*.") {
		suffix := strings.TrimPrefix(pattern, "http://*")
		if !strings.HasPrefix(origin, "http://") {
			return false
		}
		host := strings.TrimPrefix(origin, "http://")
		return strings.HasSuffix(host, suffix) && !strings.HasPrefix(host, "*")
	}

	return false
}

var secretKeys = []string{"token", "password", "secret", "key", "api_key", "auth", "credential"}

func IsSecretKey(key string) bool {
	lower := strings.ToLower(key)
	for _, sk := range secretKeys {
		if strings.Contains(lower, sk) {
			return true
		}
	}
	return false
}

func SanitizeArgs(args map[string]interface{}) string {
	if args == nil {
		return "{}"
	}

	sanitized := sanitizeMap(args)

	data, err := json.Marshal(sanitized)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func sanitizeMap(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		if IsSecretKey(k) {
			out[k] = "[REDACTED]"
		} else {
			out[k] = sanitizeValue(v)
		}
	}
	return out
}

func sanitizeValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		return sanitizeMap(val)
	case []interface{}:
		out := make([]interface{}, len(val))
		for i, elem := range val {
			out[i] = sanitizeValue(elem)
		}
		return out
	default:
		return v
	}
}
