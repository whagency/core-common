package db

import (
	"errors"
	"testing"
)

func TestNormalizePostgresSSLMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "disable", input: SSLModeDisable, want: SSLModeDisable},
		{name: "require", input: SSLModeRequire, want: SSLModeRequire},
		{name: "verify-full", input: SSLModeVerifyFull, want: SSLModeVerifyFull},
		{name: "case and space tolerant", input: "  Require ", want: SSLModeRequire},
		// A typo must stop startup. The DSN is built by string formatting, so an
		// unrecognised value would silently travel to the driver instead of
		// enforcing the transport security the setting exists to configure.
		{name: "typo is rejected", input: "requre", wantErr: true},
		{name: "enabled is rejected", input: "enabled", wantErr: true},
		{name: "empty is rejected", input: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NormalizePostgresSSLMode(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NormalizePostgresSSLMode(%q) = %q, want an error", tt.input, got)
				}
				if !errors.Is(err, ErrUnsupportedSSLMode) {
					t.Fatalf("NormalizePostgresSSLMode(%q) error = %v, want ErrUnsupportedSSLMode", tt.input, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizePostgresSSLMode(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("NormalizePostgresSSLMode(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
