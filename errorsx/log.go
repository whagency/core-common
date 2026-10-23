package errorsx

import (
	"context"

	"github.com/whagency/core-common/logger"
)

// _errorCodesToLogAsWarn contains error codes that should be logged as warnings instead of errors, as they are expected to occur in normal operation and do not indicate a problem with the service itself.
var _errorCodesToLogAsWarn = map[int]struct{}{
	InvalidRequestError:       {},
	BodyBadFormatError:        {},
	QueryParamsBadFormatError: {},
	URLParamsBadFormatError:   {},
	RateLimitExceededError:    {},
}

// _errorCodesToLogAsError contains error codes that should be logged as errors, as they indicate issues that may require attention or investigation.
var _errorCodesToLogAsError = map[int]struct{}{
	InternalServerError:      {},
	DatabaseError:            {},
	RequestTimeoutError:      {},
	CacheError:               {},
	ConcurrencyConflictError: {},
	ExternalServiceError:     {},
}

// LogErrorResponseIfNeeded logs the error response based on its code and whether it's known or not.
func LogErrorResponseIfNeeded(ctx context.Context, er *ErrorResponse) {
	if er == nil {
		return
	}

	log := logger.LogCtxWithErrorCode(ctx, er.Code)
	if _, ok := _errorCodesToLogAsWarn[er.Code]; ok {
		log.Warnf("[errorsx][LogErrorResponseIfNeeded][httpStatus:%d][code:%d][message:%s]%s", er.HTTPStatus, er.Code, er.Message, er)
		return
	}

	if _, ok := _errorCodesToLogAsError[er.Code]; ok {
		log.Errorf("[errorsx][LogErrorResponseIfNeeded][httpStatus:%d][code:%d][message:%s]%s", er.HTTPStatus, er.Code, er.Message, er)
		return
	}
}
