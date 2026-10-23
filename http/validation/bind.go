package validation

import (
	"errors"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	appErrors "github.com/whagency/core-common/errorsx"
	commonHTTP "github.com/whagency/core-common/http"
	"github.com/whagency/core-common/logger"
)

// BindAndValidateJSON unmarshals the request body into dst, validates it, and aborts on error.
func BindAndValidateJSON[T any](c *gin.Context, v *validator.Validate, dst *T, logPrefix string) bool {
	ve, err := bindRequest(c.ShouldBindJSON, v, dst, "[validation][BindAndValidateJSON]")
	return !abortOnBindingResult(c, ve, err, logPrefix, appErrors.BodyBadFormatError, appErrors.ValidationError)
}

// BindAndValidateQuery binds query params into dst, validates them, and aborts on error.
func BindAndValidateQuery[T any](c *gin.Context, v *validator.Validate, dst *T, logPrefix string) bool {
	ve, err := bindRequest(c.ShouldBindQuery, v, dst, "[validation][BindAndValidateQuery]")
	return !abortOnBindingResult(c, ve, err, logPrefix, appErrors.QueryParamsBadFormatError, appErrors.QueryParamsBadFormatError)
}

// BindAndValidateURI binds URI params into dst, validates them, and aborts on error.
func BindAndValidateURI[T any](c *gin.Context, v *validator.Validate, dst *T, logPrefix string) bool {
	ve, err := bindRequest(c.ShouldBindUri, v, dst, "[validation][BindAndValidateURI]")
	return !abortOnBindingResult(c, ve, err, logPrefix, appErrors.URLParamsBadFormatError, appErrors.URLParamsBadFormatError)
}

// bindRequest is a helper that performs binding and validation, returning validation errors and binding errors separately for proper handling.
func bindRequest[T any](bind func(any) error, v *validator.Validate, dst *T, prefix string) (validator.ValidationErrors, error) {
	if v == nil {
		return nil, fmt.Errorf("%s[err: validator is nil]", prefix)
	}

	if err := bind(dst); err != nil {
		return nil, fmt.Errorf("%s[err: %w]", prefix, err)
	}

	if err := v.Struct(dst); err != nil {
		var ve validator.ValidationErrors
		if errors.As(err, &ve) {
			return ve, nil
		}

		return nil, fmt.Errorf("%s[err: %w]", prefix, err)
	}

	return nil, nil
}

// abortOnBindingResult handles the result of binding and validation, aborting the request if necessary and returning true if aborted.
func abortOnBindingResult(
	c *gin.Context,
	ve validator.ValidationErrors,
	err error,
	logPrefix string,
	bindErrorCode int,
	validationErrorCode int,
) bool {
	ctx := c.Request.Context()

	if err != nil {
		if logPrefix != "" {
			logger.LogCtx(ctx).Errorf("%s%s", logPrefix, err)
		} else {
			logger.LogCtx(ctx).Error(err)
		}

		er := appErrors.New(bindErrorCode)
		c.AbortWithStatusJSON(er.HTTPStatus, commonHTTP.EncodeErrorResponse(ctx, er))
		return true
	}

	if ve == nil {
		return false
	}

	var er *appErrors.ErrorResponse
	if validationErrorCode == appErrors.ValidationError {
		er = appErrors.New(
			validationErrorCode,
			appErrors.WithValidationData(ToValidationData(ve)),
		)
	} else {
		er = appErrors.New(validationErrorCode)
	}

	c.AbortWithStatusJSON(er.HTTPStatus, commonHTTP.EncodeErrorResponse(ctx, er))
	return true
}
