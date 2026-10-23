package logger

import (
	"strings"
	"testing"
)

func TestBodyFieldValueForLogObjectJSON(t *testing.T) {
	got := BodyFieldValueForLog([]byte("{\"a\":1,\"b\":\"x\"}"))
	if got != "{\"a\":1,\"b\":\"x\"}" {
		t.Fatalf("expected compact JSON, got %q", got)
	}
}

func TestBodyFieldValueForLogArrayJSON(t *testing.T) {
	got := BodyFieldValueForLog([]byte("[1,2,3]"))
	if got != "[1,2,3]" {
		t.Fatalf("expected compact JSON array, got %q", got)
	}
}

// A non-JSON body is reported by size rather than echoed. It used to be echoed
// verbatim, which is how a form-encoded "secret=..." reached the log.
func TestBodyFieldValueForLogNonJSONIsNotEchoed(t *testing.T) {
	in := "secret=super-secret-value&response=token"
	got := BodyFieldValueForLog([]byte(in))
	if strings.Contains(got, "super-secret-value") {
		t.Fatalf("a non-JSON body must not be reproduced, got %q", got)
	}
	if got != "<non-json body redacted: 40 bytes>" {
		t.Fatalf("unexpected placeholder: %q", got)
	}
}

func TestBodyFieldValueForLogEmpty(t *testing.T) {
	got := BodyFieldValueForLog([]byte(" \n\t "))
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestBodyFieldValueForLogPrettyJSONCompacts(t *testing.T) {
	in := "{\n  \"contact\": {\n    \"type\": \"email\"\n  }\n}\n"
	got := BodyFieldValueForLog([]byte(in))
	if got != "{\"contact\":{\"type\":\"email\"}}" {
		t.Fatalf("expected compact JSON, got %q", got)
	}
}

func TestBodyFieldValueForLogRedactsCredentials(t *testing.T) {
	tests := []struct {
		name string
		in   string
		gone string
	}{
		{
			name: "login request",
			in:   `{"identifier":"a@b.c","password":"hunter2","recaptcha":"tok"}`,
			gone: "hunter2",
		},
		{
			name: "login response hands back both tokens",
			in:   `{"data":{"session":{"accessToken":"ey.J.x","refreshToken":"opaque-value"}}}`,
			gone: "opaque-value",
		},
		{
			name: "password change carries two",
			in:   `{"currentPassword":"old-one","newPassword":"new-one"}`,
			gone: "old-one",
		},
		{
			name: "snake case is the same field",
			in:   `{"refresh_token":"opaque-value"}`,
			gone: "opaque-value",
		},
		{
			name: "grpc session validation",
			in:   `{"token":"ey.J.x","is_expired_validation_skip":false}`,
			gone: "ey.J.x",
		},
		{
			name: "otp queue message",
			in:   `{"purpose":"otp_2fa","contactValue":"a@b.c","code":"123456"}`,
			gone: "123456",
		},
		{
			name: "nested inside an array",
			in:   `{"items":[{"password":"deep-secret"}]}`,
			gone: "deep-secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BodyFieldValueForLog([]byte(tt.in))
			if strings.Contains(got, tt.gone) {
				t.Fatalf("%q survived redaction in %q", tt.gone, got)
			}
			if !strings.Contains(got, RedactedBodyValue) {
				t.Fatalf("expected a redaction marker in %q", got)
			}
		})
	}
}

// Over-redaction costs debugging, so the things that are not credentials have to
// survive intact.
func TestBodyFieldValueForLogKeepsNonSecrets(t *testing.T) {
	in := `{"code":1024,"message":"validation failed","data":{"userId":9007199254740993,"tokenUUID":"06fd3021-b659"}}`

	got := BodyFieldValueForLog([]byte(in))

	for _, want := range []string{
		`"code":1024`,                   // a numeric error code is not an OTP
		`"message":"validation failed"`, // the reason the request failed
		`9007199254740993`,              // exact integer, not 9.007199254740992e+15
		`"tokenUUID":"06fd3021-b659"`,   // an opaque handle, useless without the code
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %s to survive, got %q", want, got)
		}
	}
	if strings.Contains(got, RedactedBodyValue) {
		t.Fatalf("nothing here is a credential, got %q", got)
	}
}
