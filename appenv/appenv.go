package appenv

import (
	"fmt"
	"strings"
)

const (
	EnvironmentLocal      = "local"
	EnvironmentDev        = "dev"
	EnvironmentStage      = "stage"
	EnvironmentProduction = "production"
	EnvironmentTest       = "test"
)

var allowedEnvironments = []string{
	EnvironmentLocal,
	EnvironmentDev,
	EnvironmentStage,
	EnvironmentProduction,
	EnvironmentTest,
}

// ValidateAppName ensures the application name is present.
func ValidateAppName(appName string) error {
	if strings.TrimSpace(appName) == "" {
		return fmt.Errorf("APP_NAME is required")
	}

	return nil
}

// NormalizeSecurityToken validates and normalizes the shared internal security
// token, returning the trimmed value.
//
// A blank token must stop startup. envconfig's required tag only rejects an
// unset variable, so SECURITY_TOKEN= leaves the token empty, and an empty
// expected token is not something http/middleware.ValidateSecurityToken can
// ever match safely: a service would be advertising internal endpoints it has
// no way to authenticate.
func NormalizeSecurityToken(token string) (string, error) {
	normalized := strings.TrimSpace(token)
	if normalized == "" {
		return "", fmt.Errorf("SECURITY_TOKEN is required and must not be empty")
	}

	return normalized, nil
}

// Normalize validates and normalizes the configured runtime environment.
func Normalize(environment string) (string, error) {
	normalized := strings.TrimSpace(environment)
	if normalized == "" {
		return "", fmt.Errorf("APP_ENVIRONMENT is required")
	}

	switch normalized {
	case EnvironmentLocal, EnvironmentDev, EnvironmentStage, EnvironmentProduction, EnvironmentTest:
		return normalized, nil
	default:
		return "", fmt.Errorf("APP_ENVIRONMENT must be one of: %s", strings.Join(allowedEnvironments, ", "))
	}
}

// IsProduction reports whether the environment is production.
func IsProduction(environment string) bool {
	return environment == EnvironmentProduction
}

// IsDevLike reports whether the environment allows non-production development shortcuts.
func IsDevLike(environment string) bool {
	switch environment {
	case EnvironmentLocal, EnvironmentDev, EnvironmentTest:
		return true
	default:
		return false
	}
}
