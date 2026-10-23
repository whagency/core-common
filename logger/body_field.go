package logger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// RedactedBodyValue replaces the value of a field carrying a credential.
const RedactedBodyValue = "<redacted>"

// _sensitiveBodyFields lists the field names whose values must never reach a log.
//
// Keys are normalised before lookup — lowercased with "_" and "-" removed — so a
// single entry covers refreshToken, refresh_token and refresh-token alike.
//
// Header sanitisation has always existed alongside this; bodies had nothing, and
// bodies are where this platform actually carries its credentials: the login
// request holds a password, the refresh request holds a refresh token, the login
// response hands both tokens straight back, and the OTP queue message carries the
// code.
var _sensitiveBodyFields = map[string]struct{}{
	"password":             {},
	"currentpassword":      {},
	"newpassword":          {},
	"oldpassword":          {},
	"passwordconfirmation": {},
	"token":                {},
	"accesstoken":          {},
	"refreshtoken":         {},
	"idtoken":              {},
	"securitytoken":        {},
	"apitoken":             {},
	"apikey":               {},
	"secret":               {},
	"clientsecret":         {},
	"privatekey":           {},
	"pepper":               {},
	"recaptcha":            {},
	"captcha":              {},
	"otp":                  {},
	"pin":                  {},
	"authorization":        {},
	"signature":            {},
}

// _stringOnlySensitiveBodyFields lists fields that are only sensitive when they
// hold a string.
//
// "code" is the one that matters: an OTP arrives as "123456" while every error
// response in the platform carries a numeric code, and redacting those would cost
// far more debugging than it buys.
var _stringOnlySensitiveBodyFields = map[string]struct{}{
	"code": {},
}

// BodyFieldValueForLog returns a string value for body logging, with the values
// of credential-carrying fields replaced.
//
// A body that is not JSON is reported by size only. The alternative — echoing it
// — is how a form-encoded "secret=..." would end up in a log, and none of the
// callers carry non-JSON payloads worth reading anyway.
func BodyFieldValueForLog(body []byte) string {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return ""
	}

	// UseNumber keeps integers exactly as written. Decoding into float64 and
	// re-encoding would turn a user id into 1.2345678901234568e+18.
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()

	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nonJSONBodyPlaceholder(trimmed)
	}

	encoded, err := encodeForLog(redactSensitiveFields(decoded))
	if err != nil {
		return nonJSONBodyPlaceholder(trimmed)
	}

	return encoded
}

// encodeForLog marshals with HTML escaping off. The default would render the
// redaction marker as \u003credacted\u003e and mangle every "&" and "<" in
// ordinary content besides -- escaping that exists for embedding JSON in HTML,
// which a log line never is.
func encodeForLog(value any) (string, error) {
	var buf bytes.Buffer

	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", err
	}

	// Encode appends a newline.
	return strings.TrimRight(buf.String(), "\n"), nil
}

// nonJSONBodyPlaceholder describes a body without reproducing it.
func nonJSONBodyPlaceholder(body []byte) string {
	return fmt.Sprintf("<non-json body redacted: %d bytes>", len(body))
}

// redactSensitiveFields walks a decoded JSON value and replaces the values of
// sensitive fields, at any depth.
func redactSensitiveFields(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			if isSensitiveBodyField(key, nested) {
				typed[key] = RedactedBodyValue
				continue
			}
			typed[key] = redactSensitiveFields(nested)
		}
		return typed
	case []any:
		for i, nested := range typed {
			typed[i] = redactSensitiveFields(nested)
		}
		return typed
	default:
		return value
	}
}

// isSensitiveBodyField reports whether a field's value must be redacted.
func isSensitiveBodyField(key string, value any) bool {
	normalized := normalizeBodyFieldKey(key)

	if _, ok := _sensitiveBodyFields[normalized]; ok {
		return true
	}

	if _, ok := _stringOnlySensitiveBodyFields[normalized]; ok {
		_, isString := value.(string)
		return isString
	}

	return false
}

// normalizeBodyFieldKey lowercases a field name and drops the separators, so one
// entry in the list covers every casing convention the platform uses.
func normalizeBodyFieldKey(key string) string {
	var b strings.Builder
	b.Grow(len(key))

	for _, r := range strings.ToLower(key) {
		if r == '_' || r == '-' {
			continue
		}
		b.WriteRune(r)
	}

	return b.String()
}
