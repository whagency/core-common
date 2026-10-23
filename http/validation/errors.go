package validation

import (
	stdErrors "errors"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"
	appErrors "github.com/whagency/core-common/errorsx"
)

var (
	tagAliasesMu sync.RWMutex
	tagAliases   = map[string]string{
		"alpha":            appErrors.ValidationTagFormat,
		"datetime":         appErrors.ValidationTagFormat,
		"email":            appErrors.ValidationTagFormat,
		"e164":             appErrors.ValidationTagFormat,
		"http_url":         appErrors.ValidationTagFormat,
		"iso3166_1_alpha2": appErrors.ValidationTagFormat,
		"not_blank":        appErrors.ValidationTagRequired,
		"uuid":             appErrors.ValidationTagUUID,
		"uuid4":            appErrors.ValidationTagUUID,
	}
)

// registerTagAlias registers a transport-facing alias for a validator tag.
func registerTagAlias(tag string, alias string) {
	tagAliasesMu.Lock()
	defer tagAliasesMu.Unlock()
	tagAliases[tag] = alias
}

// ToValidationData converts validation errors into the shared ValidationData format.
func ToValidationData(err error) appErrors.ValidationData {
	if err == nil {
		return nil
	}

	var ve validator.ValidationErrors
	if ok := stdErrors.As(err, &ve); !ok {
		return nil
	}

	out := make(appErrors.ValidationData, 0, len(ve))
	for i := range ve {
		out = append(out, appErrors.NewValidationEntry(
			fieldPath(ve[i]),
			mapTag(ve[i]),
			encodeParams(ve[i])...,
		))
	}

	return out
}

// fieldPath extracts the field path from the validation error namespace.
func fieldPath(fe validator.FieldError) string {
	ns := fe.Namespace()
	if i := strings.IndexByte(ns, '.'); i >= 0 {
		ns = ns[i+1:]
	}

	ns = strings.ReplaceAll(ns, "[", ".")
	ns = strings.ReplaceAll(ns, "]", "")

	return ns
}

// encodeParams encodes validation parameters into a slice of strings based on the validation tag.
func encodeParams(fe validator.FieldError) []string {
	switch fe.Tag() {
	case appErrors.ValidationTagOneOf:
		return strings.Fields(fe.Param())
	case appErrors.ValidationTagLen,
		appErrors.ValidationTagMin,
		appErrors.ValidationTagMax,
		appErrors.ValidationTagEQ,
		appErrors.ValidationTagNE,
		appErrors.ValidationTagGT,
		appErrors.ValidationTagGTE,
		appErrors.ValidationTagLT,
		appErrors.ValidationTagLTE:
		return []string{fe.Param()}
	default:
		return []string{}
	}
}

// mapTag maps a validation tag to its alias if available, otherwise returns the original tag.
func mapTag(fe validator.FieldError) string {
	tagAliasesMu.RLock()
	alias, ok := tagAliases[fe.Tag()]
	tagAliasesMu.RUnlock()
	if ok {
		return alias
	}

	return fe.Tag()
}
