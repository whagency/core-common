package errorsx

const (
	// 1..99999 reserved for common errors
	InvalidRequestError = iota + 1
	InternalServerError
	DatabaseError
	BodyBadFormatError
	QueryParamsBadFormatError
	RequestTimeoutError
	AccessForbiddenError
	RecordNotFoundError
	RecordAlreadyExistsError
	CacheError
	ValidationError
	RouteNotFoundError
	ConcurrencyConflictError
	ExternalServiceError
	RateLimitExceededError
	NoContentError
	UserSessionTokenInvalidError
	UserSessionExpiredError
	RestrictedOperationError
	InsufficientFundsError
	URLParamsBadFormatError
	QueuePublishError
)
