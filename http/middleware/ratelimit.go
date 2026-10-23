package middleware

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/whagency/core-common/errorsx"
	httpCommon "github.com/whagency/core-common/http"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/ratelimit"

	"github.com/gin-gonic/gin"
)

// LimitKeyFunc defines a function type that generates a rate limiting key from the Gin context.
type LimitKeyFunc func(*gin.Context) string

// GinRateLimitMiddleware is a Gin middleware that applies rate limiting based on the provided LimiterStore and KeyFunc.
// When bucket tokens are exhausted, it responds with 429 and sets Retry-After header.
func GinRateLimitMiddleware(store *ratelimit.LimiterStore, keyFn LimitKeyFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := ""
		if keyFn != nil {
			key = keyFn(c)
		}
		if key == "" {
			key = GetClientIP(c)
		}

		if !consumeRateLimit(c, store, key) {
			return
		}

		c.Next()
	}
}

// consumeRateLimit checks the rate limit for the given key using the LimiterStore. If the limit is exceeded, it aborts the request with a 429 status and sets the Retry-After header based on the reservation delay.
func consumeRateLimit(c *gin.Context, store *ratelimit.LimiterStore, key string) bool {
	reservation := store.Get(key).Reserve()
	if !reservation.OK() {
		logger.LogCtx(c.Request.Context()).Warnf("[http][middleware:RateLimit][key:%s][err: rate limit exceeded]", key)
		c.AbortWithStatusJSON(
			http.StatusTooManyRequests,
			httpCommon.EncodeErrorResponse(
				c.Request.Context(),
				errorsx.New(errorsx.RateLimitExceededError),
			),
		)
		return false
	}

	delay := reservation.Delay()
	if delay <= 0 {
		return true
	}

	reservation.CancelAt(time.Now())
	retryAfterSec := int(math.Ceil(delay.Seconds()))
	if retryAfterSec < 1 {
		retryAfterSec = 1
	}

	c.Header("Retry-After", strconv.Itoa(retryAfterSec))
	logger.LogCtx(c.Request.Context()).Warnf("[http][middleware:RateLimit][key:%s][retryAfter:%d][err: rate limit exceeded]", key, retryAfterSec)
	c.AbortWithStatusJSON(
		http.StatusTooManyRequests,
		httpCommon.EncodeErrorResponse(
			c.Request.Context(),
			errorsx.New(errorsx.RateLimitExceededError),
		),
	)
	return false
}

// GetClientIP retrieves the client's IP address from the Gin context.
func GetClientIP(c *gin.Context) string {
	ip := strings.TrimSpace(c.ClientIP())
	if ip == "" {
		ip = strings.TrimSpace(c.Request.RemoteAddr)
	}
	if ip == "" {
		ip = "unknown"
	}
	return "ip:" + ip
}
