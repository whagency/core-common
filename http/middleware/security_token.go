package middleware

import (
	"crypto/subtle"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/whagency/core-common/errorsx"
	commonHTTP "github.com/whagency/core-common/http"
)

// ValidateSecurityToken validates a shared internal security token header.
//
// An empty expectedToken rejects every request. A service whose token is not
// configured has no way to tell an authorised caller from anyone else, so the
// safe answer is to deny: otherwise a request that simply omits the header
// would match the empty expected value and pass.
//
// The comparison is constant-time so the token cannot be recovered by timing
// rejected requests.
func ValidateSecurityToken(expectedToken, headerName string) gin.HandlerFunc {
	expected := strings.TrimSpace(expectedToken)
	header := strings.TrimSpace(headerName)

	return func(c *gin.Context) {
		provided := strings.TrimSpace(c.Request.Header.Get(header))

		if expected == "" || subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) != 1 {
			er := errorsx.New(errorsx.AccessForbiddenError)
			c.AbortWithStatusJSON(er.HTTPStatus, commonHTTP.EncodeErrorResponse(c.Request.Context(), er))
			return
		}

		c.Next()
	}
}
