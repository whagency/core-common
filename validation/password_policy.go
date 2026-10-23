package validation

import (
	"fmt"
	"unicode"
)

const DefaultMinLengthPassword = 8

type PasswordPolicy struct {
	MinLength      int
	RequireUpper   bool
	RequireLower   bool
	RequireNumber  bool
	RequireSpecial bool
}

// NewDefaultPasswordPolicy returns a default password policy.
func NewDefaultPasswordPolicy(minLength int) PasswordPolicy {
	if minLength <= 0 {
		minLength = DefaultMinLengthPassword
	}

	return PasswordPolicy{
		MinLength:      minLength,
		RequireUpper:   true,
		RequireLower:   true,
		RequireNumber:  true,
		RequireSpecial: true,
	}
}

// Validate reports whether a password satisfies the policy.
func (p PasswordPolicy) Validate(password string) error {
	if len(password) < p.MinLength {
		return fmt.Errorf("[PasswordPolicy:Validate][err: password must be at least %d characters]", p.MinLength)
	}

	var hasUpper, hasLower, hasNumber, hasSpecial bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasNumber = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			hasSpecial = true
		}
	}

	if p.RequireUpper && !hasUpper {
		return fmt.Errorf("[PasswordPolicy:Validate][err: password must contain at least one uppercase letter]")
	}
	if p.RequireLower && !hasLower {
		return fmt.Errorf("[PasswordPolicy:Validate][err: password must contain at least one lowercase letter]")
	}
	if p.RequireNumber && !hasNumber {
		return fmt.Errorf("[PasswordPolicy:Validate][err: password must contain at least one number]")
	}
	if p.RequireSpecial && !hasSpecial {
		return fmt.Errorf("[PasswordPolicy:Validate][err: password must contain at least one special character]")
	}

	return nil
}
