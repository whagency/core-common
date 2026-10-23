package errorsx

import "slices"

const (
	// ValidationTagRequired marks a missing or empty required field.
	ValidationTagRequired = "required"
	// ValidationTagOneOf marks a value outside the allowed fixed set.
	ValidationTagOneOf = "oneof"
	// ValidationTagFormat marks a field with an invalid transport or domain format.
	ValidationTagFormat = "format"
	// ValidationTagUUID marks an invalid UUID value.
	ValidationTagUUID = "uuid"
	// ValidationTagLen marks an exact length constraint.
	ValidationTagLen = "len"
	// ValidationTagMin marks a minimum value or length constraint.
	ValidationTagMin = "min"
	// ValidationTagMax marks a maximum value or length constraint.
	ValidationTagMax = "max"
	// ValidationTagGT marks a value that must be strictly greater than the provided limit.
	ValidationTagGT = "gt"
	// ValidationTagGTE marks a value that must be greater than or equal to the provided limit.
	ValidationTagGTE = "gte"
	// ValidationTagLT marks a value that must be strictly less than the provided limit.
	ValidationTagLT = "lt"
	// ValidationTagLTE marks a value that must be less than or equal to the provided limit.
	ValidationTagLTE = "lte"
	// ValidationTagEQ marks an exact equality constraint.
	ValidationTagEQ = "eq"
	// ValidationTagNE marks a non-equality constraint.
	ValidationTagNE = "ne"
	// ValidationTagWeak marks a value that fails a strength policy.
	ValidationTagWeak = "weak"
	// ValidationTagInvalidValue marks a business-invalid value that is syntactically valid.
	ValidationTagInvalidValue = "invalid_value"
	// ValidationTagMaxSizeBytes marks a payload size limit in bytes.
	ValidationTagMaxSizeBytes = "max_size_bytes"
	// ValidationTagMaxFractionalDigits marks a decimal precision constraint.
	ValidationTagMaxFractionalDigits = "max_fractional_digits"
	// ValidationTagMimeExtensionMismatch marks a mismatch between MIME type and file extension.
	ValidationTagMimeExtensionMismatch = "mime_extension_mismatch"
)

// ValidationData represents a list of validation errors.
type ValidationData []ValidationEntry

// ValidationEntry represents a single field validation error.
type ValidationEntry struct {
	Field  string
	Tag    string
	Params []string
}

// NewValidationEntry builds a validation entry and clones params for safe reuse.
func NewValidationEntry(field, tag string, params ...string) ValidationEntry {
	return ValidationEntry{
		Field:  field,
		Tag:    tag,
		Params: slices.Clone(params),
	}
}

// NewOneOfValidationEntry builds a validation entry for allowed value checks.
func NewOneOfValidationEntry(field string, values ...string) ValidationEntry {
	return NewValidationEntry(field, ValidationTagOneOf, values...)
}

// NewInvalidValueValidationEntry builds a validation entry for business-invalid values.
func NewInvalidValueValidationEntry(field string, params ...string) ValidationEntry {
	return NewValidationEntry(field, ValidationTagInvalidValue, params...)
}

// WithValidationEntry attaches a single validation entry to the response.
func WithValidationEntry(field, tag string, params ...string) Option {
	return WithValidationEntries(NewValidationEntry(field, tag, params...))
}

// WithOneOfValidation attaches a oneof validation entry to the response.
func WithOneOfValidation(field string, values ...string) Option {
	return WithValidationEntries(NewOneOfValidationEntry(field, values...))
}

// WithInvalidValueValidation attaches an invalid_value validation entry to the response.
func WithInvalidValueValidation(field string, params ...string) Option {
	return WithValidationEntries(NewInvalidValueValidationEntry(field, params...))
}

// WithValidationEntries attaches validation entries to the response.
func WithValidationEntries(entries ...ValidationEntry) Option {
	return WithValidationData(entries)
}

// WithValidationData attaches validation errors to the response.
func WithValidationData(ve ValidationData) Option {
	return func(er *ErrorResponse) {
		data := make([]map[string]any, 0, len(ve))
		for _, v := range ve {
			entry := map[string]any{
				"field":  v.Field,
				"tag":    v.Tag,
				"params": cloneValidationParams(v.Params),
			}
			data = append(data, entry)
		}
		if er.Data == nil {
			er.Data = make(ErrorData)
		}
		er.Data["errors"] = data
	}
}

// cloneValidationParams creates a copy of the validation parameters to prevent unintended mutations.
func cloneValidationParams(params []string) []string {
	if len(params) == 0 {
		return []string{}
	}

	return slices.Clone(params)
}
