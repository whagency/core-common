package appenv

import "testing"

func TestValidateAppName(t *testing.T) {
	t.Parallel()

	if err := ValidateAppName("account-service"); err != nil {
		t.Fatalf("expected app name to be valid, got error: %v", err)
	}

	if err := ValidateAppName("   "); err == nil {
		t.Fatal("expected blank app name to be rejected")
	}
}

func TestNormalizeSecurityToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "token is kept", input: "s3cret", want: "s3cret"},
		{name: "token is trimmed", input: "  s3cret  ", want: "s3cret"},
		// envconfig's required tag only rejects an unset variable. SECURITY_TOKEN=
		// counts as set, and an empty expected token cannot authenticate anything.
		{name: "empty is rejected", input: "", wantErr: true},
		{name: "whitespace-only is rejected", input: "   ", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NormalizeSecurityToken(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NormalizeSecurityToken(%q) = %q, want an error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeSecurityToken(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeSecurityToken(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "local", input: EnvironmentLocal, want: EnvironmentLocal},
		{name: "dev", input: EnvironmentDev, want: EnvironmentDev},
		{name: "stage", input: EnvironmentStage, want: EnvironmentStage},
		{name: "production", input: EnvironmentProduction, want: EnvironmentProduction},
		{name: "test", input: EnvironmentTest, want: EnvironmentTest},
		{name: "trimmed", input: "  local  ", want: EnvironmentLocal},
		{name: "missing", input: "", wantErr: true},
		{name: "unsupported", input: "debug", wantErr: true},
		{name: "unsupported alias", input: "development", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Normalize(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.input)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.input, err)
			}

			if got != tt.want {
				t.Fatalf("Normalize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsProduction(t *testing.T) {
	t.Parallel()

	if !IsProduction(EnvironmentProduction) {
		t.Fatal("expected production environment to be detected")
	}

	if IsProduction(EnvironmentDev) {
		t.Fatal("did not expect dev environment to be production")
	}
}

func TestIsDevLike(t *testing.T) {
	t.Parallel()

	if !IsDevLike(EnvironmentLocal) {
		t.Fatal("expected local to be dev-like")
	}

	if !IsDevLike(EnvironmentDev) {
		t.Fatal("expected dev to be dev-like")
	}

	if !IsDevLike(EnvironmentTest) {
		t.Fatal("expected test to be dev-like")
	}

	if IsDevLike(EnvironmentStage) {
		t.Fatal("did not expect stage to be dev-like")
	}
}
