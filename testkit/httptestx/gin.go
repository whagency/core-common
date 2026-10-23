package httptestx

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// InitGin switches Gin into test mode.
func InitGin(t testing.TB) {
	t.Helper()

	gin.SetMode(gin.TestMode)
}

// NewGinContext creates a Gin context backed by an httptest recorder.
func NewGinContext(
	t testing.TB,
	method string,
	target string,
	body io.Reader,
	headers map[string]string,
) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	InitGin(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(method, target, body)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	ctx.Request = req

	return ctx, recorder
}

// NewJSONGinContext creates a Gin context with a JSON request body.
func NewJSONGinContext(
	t testing.TB,
	method string,
	target string,
	body any,
) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}
		reader = bytes.NewReader(raw)
	}

	return NewGinContext(t, method, target, reader, map[string]string{
		"Content-Type": "application/json",
	})
}

// DecodeRecorderJSON decodes a JSON httptest recorder body into the target type.
func DecodeRecorderJSON[T any](t testing.TB, recorder *httptest.ResponseRecorder) T {
	t.Helper()

	var out T
	if err := json.Unmarshal(recorder.Body.Bytes(), &out); err != nil {
		t.Fatalf("json.Unmarshal failed: %v; body=%s", err, recorder.Body.String())
	}
	return out
}
