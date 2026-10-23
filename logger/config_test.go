package logger

import "testing"

func TestNormalizeStdoutFormat(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "default empty", in: "", want: StdoutFormatConsole},
		{name: "console", in: "console", want: StdoutFormatConsole},
		{name: "json", in: "json", want: StdoutFormatJSON},
		{name: "trim and lowercase", in: "  JSON  ", want: StdoutFormatJSON},
		{name: "invalid", in: "text", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeStdoutFormat(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.in)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.in, err)
			}

			if got != tt.want {
				t.Fatalf("normalizeStdoutFormat(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestValidateAndSetConfigNormalizesStdoutFormat(t *testing.T) {
	prevCfg := config
	t.Cleanup(func() {
		config = prevCfg
	})

	err := validateAndSetConfig(Config{
		AppName:      "svc",
		Environment:  "stage",
		Level:        "info",
		StdoutFormat: "  JSON  ",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if config.StdoutFormat != StdoutFormatJSON {
		t.Fatalf("config.StdoutFormat = %q, want %q", config.StdoutFormat, StdoutFormatJSON)
	}
}

func TestValidateAndSetConfigRejectsInvalidStdoutFormat(t *testing.T) {
	prevCfg := config
	t.Cleanup(func() {
		config = prevCfg
	})

	err := validateAndSetConfig(Config{
		AppName:      "svc",
		Environment:  "stage",
		Level:        "info",
		StdoutFormat: "xml",
	})
	if err == nil {
		t.Fatal("expected error for invalid StdoutFormat")
	}
}
