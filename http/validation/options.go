package validation

import "github.com/go-playground/validator/v10"

// Option mutates a validator instance during construction.
type Option func(*validator.Validate) error

// WithValidation registers a custom field validation under the provided tag.
func WithValidation(tag string, fn validator.Func) Option {
	return func(v *validator.Validate) error {
		return v.RegisterValidation(tag, fn)
	}
}

// WithTagAlias registers a transport-facing alias for a validator tag.
func WithTagAlias(tag string, alias string) Option {
	return func(_ *validator.Validate) error {
		registerTagAlias(tag, alias)
		return nil
	}
}

// WithValidationAlias registers a custom validation and its transport-facing alias.
func WithValidationAlias(tag string, alias string, fn validator.Func) Option {
	return func(v *validator.Validate) error {
		registerTagAlias(tag, alias)
		return v.RegisterValidation(tag, fn)
	}
}
