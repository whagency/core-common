package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

const _testTokenHeader = "X-API-KEY"

func TestValidateSecurityToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		expectedToken string
		headerName    string
		sendHeader    bool
		sentToken     string
		wantStatus    int
	}{
		{
			name:          "matching token passes",
			expectedToken: "s3cret",
			headerName:    _testTokenHeader,
			sendHeader:    true,
			sentToken:     "s3cret",
			wantStatus:    http.StatusOK,
		},
		{
			name:          "surrounding whitespace is ignored on both sides",
			expectedToken: "  s3cret  ",
			headerName:    "  " + _testTokenHeader + "  ",
			sendHeader:    true,
			sentToken:     " s3cret ",
			wantStatus:    http.StatusOK,
		},
		{
			name:          "wrong token is rejected",
			expectedToken: "s3cret",
			headerName:    _testTokenHeader,
			sendHeader:    true,
			sentToken:     "guess",
			wantStatus:    http.StatusForbidden,
		},
		{
			name:          "missing header is rejected",
			expectedToken: "s3cret",
			headerName:    _testTokenHeader,
			sendHeader:    false,
			wantStatus:    http.StatusForbidden,
		},
		{
			// The fail-open case: an unconfigured token must not turn a request
			// that sends no header into an authorised one.
			name:          "unconfigured token rejects request without header",
			expectedToken: "",
			headerName:    _testTokenHeader,
			sendHeader:    false,
			wantStatus:    http.StatusForbidden,
		},
		{
			name:          "unconfigured token rejects request with empty header",
			expectedToken: "",
			headerName:    _testTokenHeader,
			sendHeader:    true,
			sentToken:     "",
			wantStatus:    http.StatusForbidden,
		},
		{
			name:          "whitespace-only token counts as unconfigured",
			expectedToken: "   ",
			headerName:    _testTokenHeader,
			sendHeader:    true,
			sentToken:     "   ",
			wantStatus:    http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gin.SetMode(gin.TestMode)

			router := gin.New()
			router.Use(ValidateSecurityToken(tt.expectedToken, tt.headerName))
			router.GET("/internal/ping", func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/internal/ping", nil)
			if tt.sendHeader {
				req.Header.Set(_testTokenHeader, tt.sentToken)
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}
