package handlers

import (
	"net/http"

	"github.com/whagency/core-common/errorsx"
	httpCommon "github.com/whagency/core-common/http"

	"github.com/gin-gonic/gin"
)

// RouteNotFound handles undefined routes.
//
//	@Summary        Catch-all for undefined routes
//	@Description    Returns a structured 404 payload when a route is not registered.
//	@Tags           errors
//	@Produce        json
//	@Failure        404 {object} httpCommon.Response "Route not found."
//	@Router         /{any} [get]
func RouteNotFound(c *gin.Context) {
	c.AbortWithStatusJSON(
		http.StatusNotFound,
		httpCommon.EncodeErrorResponse(
			c.Request.Context(),
			errorsx.New(errorsx.RouteNotFoundError),
		),
	)
}
