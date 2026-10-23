package validation

import (
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

// New constructs a validator configured for HTTP transport structs.
func New(opts ...Option) (*validator.Validate, error) {
	v := validator.New(validator.WithRequiredStructEnabled())
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		return transportFieldName(fld)
	})
	if err := v.RegisterValidation("not_blank", func(fl validator.FieldLevel) bool {
		return strings.TrimSpace(fl.Field().String()) != ""
	}); err != nil {
		return nil, err
	}

	for _, opt := range opts {
		if err := opt(v); err != nil {
			return nil, err
		}
	}

	return v, nil
}

// MustNew constructs a validator and panics on configuration errors.
func MustNew(opts ...Option) *validator.Validate {
	v, err := New(opts...)
	if err != nil {
		panic(err)
	}

	return v
}

// transportFieldName returns the transport field name for a struct field.
func transportFieldName(fld reflect.StructField) string {
	for _, tagName := range []string{"json", "form", "uri"} {
		if name := parseTransportTagName(fld.Tag.Get(tagName)); name != "" {
			return name
		}
	}

	return fld.Name
}

// parseTransportTagName extracts the transport field name from a struct field tag.
func parseTransportTagName(value string) string {
	if value == "" || value == "-" {
		return ""
	}

	name := strings.SplitN(value, ",", 2)[0]
	if name == "" || name == "-" {
		return ""
	}

	return name
}
