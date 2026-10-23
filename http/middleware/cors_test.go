package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestCORS_Default_Preflight(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(CORS())
	router.GET("/ping", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodOptions, "/ping", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, _defaultAllowedHeaders, w.Header().Get("Access-Control-Allow-Headers"))
	assert.Equal(t, _defaultAllowedMethods, w.Header().Get("Access-Control-Allow-Methods"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
}

func TestCORSWithConfig_ExplicitOriginWithCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := DefaultCORSConfig()
	cfg.AllowedOrigins = []string{"https://app.example.com"}
	cfg.AllowCredentials = true

	cors, err := CORSWithConfig(cfg)
	if !assert.NoError(t, err) {
		return
	}

	router := gin.New()
	router.Use(cors)
	router.GET("/ping", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodOptions, "/ping", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "https://app.example.com", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
	assert.True(t, strings.Contains(w.Header().Get("Vary"), "Origin"))
}

func TestCORSWithConfig_WildcardAndCredentials_Invalid(t *testing.T) {
	cfg := DefaultCORSConfig()
	cfg.AllowCredentials = true

	_, err := CORSWithConfig(cfg)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidCORSConfig))
}

func TestCORSWithConfig_DisallowedOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := DefaultCORSConfig()
	cfg.AllowedOrigins = []string{"https://app.example.com"}

	cors, err := CORSWithConfig(cfg)
	if !assert.NoError(t, err) {
		return
	}

	router := gin.New()
	router.Use(cors)
	router.GET("/ping", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
}

func TestParseCSV(t *testing.T) {
	result := ParseCSV(" https://a.example.com , , https://b.example.com ")
	assert.Equal(t, []string{"https://a.example.com", "https://b.example.com"}, result)
}
