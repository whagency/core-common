package http

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/whagency/core-common/errorsx"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/pagination"
)

const (
	_successCode    = 0
	_successMessage = "success"
)

// Response represents the standard response structure
type Response struct {
	Code    int          `json:"code"`                      // 0 indicates success, > 0 indicates an error
	Message string       `json:"message"`                   // "success" for successful responses, error message for errors
	Data    any          `json:"data" swaggertype:"object"` // The actual response data object
	Meta    metaResponse `json:"meta"`                      // Metadata about the response
}

// metaResponse represents the metadata included in every response
type metaResponse struct {
	RequestID string `json:"requestId" example:"010a0998-4b1a-4e70-a951-d27a40b49c0c"`
	Timestamp string `json:"timestamp" example:"2025-10-07T12:00:45.327647382Z"`
}

type Pagination struct {
	ShownFrom  int   `json:"shownFrom"`
	ShownTo    int   `json:"shownTo"`
	Page       int   `json:"page"`
	PageSize   int   `json:"pageSize"`
	TotalCount int64 `json:"totalCount"`
}

// EncodePaginationResponse encodes the pagination.Pagination into a response format
func EncodePaginationResponse(p pagination.Pagination) Pagination {
	return Pagination{
		ShownFrom:  p.ShownFrom,
		ShownTo:    p.ShownTo,
		Page:       p.Page,
		PageSize:   p.PageSize,
		TotalCount: p.TotalCount,
	}
}

// EncodeSuccessResponse prepares the success response
func EncodeSuccessResponse(ctx context.Context, data any) Response {
	return Response{
		Code:    _successCode,
		Message: _successMessage,
		Data:    encodeData(data),
		Meta:    encodeMetaResponse(ctx),
	}
}

// EncodeErrorResponse prepares the error response
func EncodeErrorResponse(ctx context.Context, er *errorsx.ErrorResponse) Response {
	return Response{
		Code:    er.Code,
		Message: er.Message,
		Data:    encodeErrorData(er.Data),
		Meta:    encodeMetaResponse(ctx),
	}
}

// encodeErrorData ensures the error data is not nil
func encodeErrorData(data map[string]any) map[string]any {
	if data == nil {
		return map[string]any{}
	}
	return data
}

// encodeData ensures the response data is not nil
func encodeData(data any) any {
	if data == nil {
		return struct{}{}
	}
	return data
}

// encodeMetaResponse encodes the meta response
func encodeMetaResponse(ctx context.Context) metaResponse {
	return metaResponse{
		RequestID: logger.GetRequestID(ctx),
		Timestamp: time.Now().Format(time.RFC3339),
	}
}

// AbortIfError aborts the request with an error response if err is not nil.
func AbortIfError(c *gin.Context, ctx context.Context, err error) bool {
	if err == nil {
		return false
	}

	if er, ok := errorsx.AsErrorResponse(err); ok {
		errorsx.LogErrorResponseIfNeeded(ctx, er)
		c.AbortWithStatusJSON(er.HTTPStatus, EncodeErrorResponse(ctx, er))
		return true
	}

	logger.LogCtx(ctx).Errorf("[http][AbortIfError][unexpected error]%s", err)

	er := errorsx.New(errorsx.InternalServerError)
	c.AbortWithStatusJSON(er.HTTPStatus, EncodeErrorResponse(ctx, er))
	return true
}
