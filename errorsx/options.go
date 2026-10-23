package errorsx

// Option defines a functional option for configuring an ErrorResponse.
type Option func(*ErrorResponse)

// WithCause attaches the underlying error that triggered the response.
func WithCause(err error) Option {
	return func(er *ErrorResponse) { er.Cause = err }
}

// WithData attaches business-specific details to the response.
func WithData(data map[string]any) Option {
	return func(er *ErrorResponse) { er.Data = data }
}
