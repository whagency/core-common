package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	_defaultAllowedOrigins = "*"
	_defaultAllowedMethods = "GET, POST, PUT, PATCH, DELETE, OPTIONS"
	_defaultAllowedHeaders = "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, Accept, Origin, Cache-Control, X-Requested-With"
)

var ErrInvalidCORSConfig = errors.New("invalid CORS config")

// CORSConfig contains configurable CORS settings that are safe to reuse across services.
type CORSConfig struct {
	AllowedOrigins   []string
	AllowCredentials bool
	AllowedMethods   string
	AllowedHeaders   string
	ExposedHeaders   string
	MaxAge           time.Duration
}

// DefaultCORSConfig returns safe default CORS values.
func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowedOrigins: []string{_defaultAllowedOrigins},
		AllowedMethods: _defaultAllowedMethods,
		AllowedHeaders: _defaultAllowedHeaders,
	}
}

// CORS returns middleware configured with DefaultCORSConfig.
func CORS() gin.HandlerFunc {
	handler, err := CORSWithConfig(DefaultCORSConfig())
	if err != nil {
		// Safe fallback for misconfiguration path: keep server alive.
		return func(c *gin.Context) {
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
			c.Next()
		}
	}
	return handler
}

// CORSWithConfig returns a configurable CORS middleware.
func CORSWithConfig(cfg CORSConfig) (gin.HandlerFunc, error) {
	normalizedCfg, err := normalizeCORSConfig(cfg)
	if err != nil {
		return nil, err
	}

	return func(c *gin.Context) {
		header := c.Writer.Header()

		origin := strings.TrimSpace(c.Request.Header.Get("Origin"))
		allowOrigin, isAllowed := resolveAllowedOrigin(origin, normalizedCfg.AllowedOrigins)
		if isAllowed {
			header.Set("Access-Control-Allow-Origin", allowOrigin)
			if allowOrigin != "*" {
				header.Set("Vary", mergeVaryValues(header.Get("Vary"), "Origin"))
			}

			header.Set("Access-Control-Allow-Headers", normalizedCfg.AllowedHeaders)
			header.Set("Access-Control-Allow-Methods", normalizedCfg.AllowedMethods)

			if normalizedCfg.ExposedHeaders != "" {
				header.Set("Access-Control-Expose-Headers", normalizedCfg.ExposedHeaders)
			}
			if normalizedCfg.AllowCredentials && allowOrigin != "*" {
				header.Set("Access-Control-Allow-Credentials", "true")
			}

			if c.Request.Method == http.MethodOptions {
				header.Set("Vary", mergeVaryValues(header.Get("Vary"), "Access-Control-Request-Method"))
				header.Set("Vary", mergeVaryValues(header.Get("Vary"), "Access-Control-Request-Headers"))
				if normalizedCfg.MaxAge > 0 {
					header.Set("Access-Control-Max-Age", strconv.FormatInt(int64(normalizedCfg.MaxAge/time.Second), 10))
				}
			}
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}, nil
}

// normalizeCORSConfig validates and normalizes the CORS configuration, applying defaults where necessary.
func normalizeCORSConfig(cfg CORSConfig) (CORSConfig, error) {
	result := cfg

	if len(result.AllowedOrigins) == 0 {
		result.AllowedOrigins = []string{_defaultAllowedOrigins}
	}
	result.AllowedOrigins = normalizeList(result.AllowedOrigins)
	if len(result.AllowedOrigins) == 0 {
		result.AllowedOrigins = []string{_defaultAllowedOrigins}
	}

	if strings.TrimSpace(result.AllowedMethods) == "" {
		result.AllowedMethods = _defaultAllowedMethods
	}
	if strings.TrimSpace(result.AllowedHeaders) == "" {
		result.AllowedHeaders = _defaultAllowedHeaders
	}
	result.ExposedHeaders = strings.TrimSpace(result.ExposedHeaders)

	if result.MaxAge < 0 {
		return CORSConfig{}, fmt.Errorf("[cors][normalizeCORSConfig][err:%w][reason:max age must be >= 0]", ErrInvalidCORSConfig)
	}
	if result.AllowCredentials && containsWildcard(result.AllowedOrigins) {
		return CORSConfig{}, fmt.Errorf("[cors][normalizeCORSConfig][err:%w][reason:wildcard origin cannot be used with credentials]", ErrInvalidCORSConfig)
	}

	return result, nil
}

// normalizeList trims whitespace, removes empty values, and deduplicates a list of strings.
func normalizeList(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))

	for i := range items {
		item := strings.TrimSpace(items[i])
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}

	return result
}

// containsWildcard checks if the list of allowed origins contains a wildcard entry.
func containsWildcard(allowedOrigins []string) bool {
	for i := range allowedOrigins {
		if allowedOrigins[i] == "*" {
			return true
		}
	}
	return false
}

// resolveAllowedOrigin determines if the request origin is allowed based on the configured allowed origins and returns the appropriate Access-Control-Allow-Origin value.
func resolveAllowedOrigin(origin string, allowedOrigins []string) (string, bool) {
	if len(allowedOrigins) == 0 {
		return "", false
	}

	if containsWildcard(allowedOrigins) {
		return "*", true
	}

	if strings.TrimSpace(origin) == "" {
		return "", false
	}

	for i := range allowedOrigins {
		if origin == allowedOrigins[i] {
			return origin, true
		}
	}

	return "", false
}

// ParseCSV parses a comma-separated string into a list with trimmed non-empty values.
func ParseCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for i := range parts {
		item := strings.TrimSpace(parts[i])
		if item == "" {
			continue
		}
		result = append(result, item)
	}
	return result
}

// mergeVaryValues merges a new value into the existing Vary header value, ensuring no duplicates and proper formatting.
func mergeVaryValues(current string, value string) string {
	if current == "" {
		return value
	}

	for _, item := range strings.Split(current, ",") {
		if strings.EqualFold(strings.TrimSpace(item), value) {
			return current
		}
	}

	return current + ", " + value
}
