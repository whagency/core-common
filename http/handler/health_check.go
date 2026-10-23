package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// HealthCheck is a handler that returns a 200 OK status code.
//
//	@Summary        Service health check
//	@Description    Basic liveness probe for load balancers and uptime monitors.
//	@Tags           monitoring
//	@Produce        json
//	@Success        200 {object} map[string]string "Service is running."
//	@Router         /health [get]
func HealthCheck(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, gin.H{"status": "OK"})
}
