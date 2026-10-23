package httptestx

import (
	"net/http"
	"testing"
)

func TestNewJSONGinContextBuildsJSONRequest(t *testing.T) {
	ctx, recorder := NewJSONGinContext(t, http.MethodPost, "/test", map[string]any{
		"name": "app",
	})

	if ctx.Request.Method != http.MethodPost {
		t.Fatalf("expected method POST, got %s", ctx.Request.Method)
	}
	if ctx.Request.URL.Path != "/test" {
		t.Fatalf("expected path /test, got %s", ctx.Request.URL.Path)
	}
	if got := ctx.GetHeader("Content-Type"); got != "application/json" {
		t.Fatalf("expected JSON content type, got %q", got)
	}

	recorder.WriteHeader(http.StatusAccepted)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected recorder status 202, got %d", recorder.Code)
	}
}

func TestDecodeRecorderJSON(t *testing.T) {
	_, recorder := NewGinContext(t, http.MethodGet, "/test", http.NoBody, nil)
	recorder.Body.WriteString(`{"name":"app"}`)

	result := DecodeRecorderJSON[struct {
		Name string `json:"name"`
	}](t, recorder)

	if result.Name != "app" {
		t.Fatalf("expected name app, got %q", result.Name)
	}
}
