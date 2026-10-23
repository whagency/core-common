package errorsx

import "net/http"

// Template describes registered error code metadata.
type Template struct {
	Code       int
	Message    string
	HTTPStatus int
}

// CommonTemplates returns a map of common error templates for typical application errors.
func CommonTemplates() map[int]Template {
	return map[int]Template{
		InvalidRequestError: {
			Code:       InvalidRequestError,
			Message:    InvalidRequestErrMessage,
			HTTPStatus: http.StatusBadRequest,
		},
		InternalServerError: {
			Code:       InternalServerError,
			Message:    InternalServerErrMessage,
			HTTPStatus: http.StatusInternalServerError,
		},
		DatabaseError: {
			Code:       DatabaseError,
			Message:    DatabaseErrMessage,
			HTTPStatus: http.StatusInternalServerError,
		},
		BodyBadFormatError: {
			Code:       BodyBadFormatError,
			Message:    BodyBadFormatErrMessage,
			HTTPStatus: http.StatusBadRequest,
		},
		QueryParamsBadFormatError: {
			Code:       QueryParamsBadFormatError,
			Message:    QueryParamsBadFormatErrMessage,
			HTTPStatus: http.StatusBadRequest,
		},
		URLParamsBadFormatError: {
			Code:       URLParamsBadFormatError,
			Message:    URLParamsBadFormatErrMessage,
			HTTPStatus: http.StatusBadRequest,
		},
		RequestTimeoutError: {
			Code:       RequestTimeoutError,
			Message:    RequestTimeoutErrMessage,
			HTTPStatus: http.StatusGatewayTimeout,
		},
		AccessForbiddenError: {
			Code:       AccessForbiddenError,
			Message:    AccessForbiddenErrMessage,
			HTTPStatus: http.StatusForbidden,
		},
		RecordNotFoundError: {
			Code:       RecordNotFoundError,
			Message:    RecordNotFoundErrMessage,
			HTTPStatus: http.StatusNotFound,
		},
		RecordAlreadyExistsError: {
			Code:       RecordAlreadyExistsError,
			Message:    RecordAlreadyExistsErrMessage,
			HTTPStatus: http.StatusConflict,
		},
		CacheError: {
			Code:       CacheError,
			Message:    CacheErrMessage,
			HTTPStatus: http.StatusInternalServerError,
		},
		ValidationError: {
			Code:       ValidationError,
			Message:    ValidationErrMessage,
			HTTPStatus: http.StatusUnprocessableEntity,
		},
		RouteNotFoundError: {
			Code:       RouteNotFoundError,
			Message:    RouteNotFoundErrMessage,
			HTTPStatus: http.StatusNotFound,
		},
		ConcurrencyConflictError: {
			Code:       ConcurrencyConflictError,
			Message:    ConcurrencyConflictErrMessage,
			HTTPStatus: http.StatusConflict,
		},
		ExternalServiceError: {
			Code:       ExternalServiceError,
			Message:    ExternalServiceErrMessage,
			HTTPStatus: http.StatusBadGateway,
		},
		UserSessionTokenInvalidError: {
			Code:       UserSessionTokenInvalidError,
			Message:    UserSessionTokenInvalidErrMessage,
			HTTPStatus: http.StatusUnauthorized,
		},
		UserSessionExpiredError: {
			Code:       UserSessionExpiredError,
			Message:    UserSessionExpiredErrMessage,
			HTTPStatus: http.StatusUnauthorized,
		},
		RestrictedOperationError: {
			Code:       RestrictedOperationError,
			Message:    RestrictedOperationErrMessage,
			HTTPStatus: http.StatusForbidden,
		},
		RateLimitExceededError: {
			Code:       RateLimitExceededError,
			Message:    RateLimitExceededErrMessage,
			HTTPStatus: http.StatusTooManyRequests,
		},
		NoContentError: {
			Code:       NoContentError,
			Message:    NoContentErrMessage,
			HTTPStatus: http.StatusNoContent,
		},
		InsufficientFundsError: {
			Code:       InsufficientFundsError,
			Message:    InsufficientFundsErrorMessage,
			HTTPStatus: http.StatusBadRequest,
		},
		QueuePublishError: {
			Code:       QueuePublishError,
			Message:    QueuePublishErrorMessage,
			HTTPStatus: http.StatusInternalServerError,
		},
	}
}
