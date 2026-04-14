package validator

import (
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"
)

// Validate is a shared validator instance. Reusing it caches struct info.
var Validate = validator.New()

// ValidationErrors converts validator.ValidationErrors into a human-readable
// map of field -> message, suitable for returning in a 422 response.
func ValidationErrors(err error) map[string]string {
	var ve validator.ValidationErrors
	if ok := errorAs(err, &ve); !ok {
		return map[string]string{"_": err.Error()}
	}

	out := make(map[string]string, len(ve))
	for _, fe := range ve {
		field := strings.ToLower(fe.Field())
		out[field] = fieldMessage(fe)
	}
	return out
}

func fieldMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required"
	case "email":
		return "must be a valid email address"
	case "min":
		return fmt.Sprintf("must be at least %s characters", fe.Param())
	case "max":
		return fmt.Sprintf("must be at most %s characters", fe.Param())
	case "e164":
		return "must be a valid E.164 phone number (e.g. +447911123456)"
	default:
		return fmt.Sprintf("failed validation: %s", fe.Tag())
	}
}

// errorAs is a helper to avoid importing errors directly in this package.
func errorAs(err error, target any) bool {
	if err == nil {
		return false
	}
	type asInterface interface {
		As(any) bool
	}
	if ai, ok := err.(asInterface); ok {
		return ai.As(target)
	}
	// fallback via errors.As
	switch t := target.(type) {
	case *validator.ValidationErrors:
		if ve, ok := err.(validator.ValidationErrors); ok {
			*t = ve
			return true
		}
	}
	return false
}
