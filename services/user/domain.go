package user

import (
	"context"
	"fmt"
)

var (
	// ErrSessionInvalid indicates that the session token is invalid.
	ErrSessionInvalid = fmt.Errorf("session token is invalid")
	// ErrSessionExpired indicates that the session token has expired.
	ErrSessionExpired = fmt.Errorf("session token has expired")
	// ErrUserNotFound indicates that the user profile associated with the session token was not found.
	ErrUserNotFound = fmt.Errorf("user not found")
)

type Status uint8

const (
	StatusPendingContactVerification Status = iota + 1
	StatusActive
	StatusBlockedUser
	StatusBlockedSystem
	StatusBlockedAdmin
	StatusDeleted
)

var _userStatusToString = map[Status]string{
	StatusPendingContactVerification: "pending_contact_verification",
	StatusActive:                     "active",
	StatusBlockedUser:                "blocked_user",
	StatusBlockedSystem:              "blocked_system",
	StatusBlockedAdmin:               "blocked_admin",
	StatusDeleted:                    "deleted",
}

var _userStatusFromString = map[string]Status{
	"pending_contact_verification": StatusPendingContactVerification,
	"active":                       StatusActive,
	"blocked_user":                 StatusBlockedUser,
	"blocked_system":               StatusBlockedSystem,
	"blocked_admin":                StatusBlockedAdmin,
	"deleted":                      StatusDeleted,
}

// NewUserStatusFromString returns UserStatus of the given string representation.
func NewUserStatusFromString(s string) (Status, error) {
	if val, ok := _userStatusFromString[s]; ok {
		return val, nil
	}
	return 0, fmt.Errorf("[NewUserStatusFromString][err: unknown status: %s]", s)
}

// String returns the string representation of the UserStatus.
func (s Status) String() string {
	if val, ok := _userStatusToString[s]; ok {
		return val
	}
	return "unknown"
}

// Uint8 returns the uint8 representation of the UserStatus.
func (s Status) Uint8() uint8 {
	return uint8(s)
}

type VerificationLevel uint8

const (
	VerificationLevelNon VerificationLevel = iota
	VerificationLevelBasic
	VerificationLevelAdvanced
	VerificationLevelPro
)

var _userVerificationLevelToString = map[VerificationLevel]string{
	VerificationLevelNon:      "non_verified",
	VerificationLevelBasic:    "basic",
	VerificationLevelAdvanced: "advanced",
	VerificationLevelPro:      "pro",
}

var _userVerificationLevelFromString = map[string]VerificationLevel{
	"non_verified": VerificationLevelNon,
	"basic":        VerificationLevelBasic,
	"advanced":     VerificationLevelAdvanced,
	"pro":          VerificationLevelPro,
}

// NewUserVerificationLevelFromString returns UserVerificationLevel of the given string representation.
func NewUserVerificationLevelFromString(s string) (VerificationLevel, error) {
	if val, ok := _userVerificationLevelFromString[s]; ok {
		return val, nil
	}
	return 0, fmt.Errorf("[NewUserVerificationLevelFromString][err: unknown verification level: %s]", s)
}

// Uint8 returns the uint8 representation of the verification level.
func (s VerificationLevel) Uint8() uint8 {
	return uint8(s)
}

// String returns the string representation of the UserVerificationLevel.
func (s VerificationLevel) String() string {
	if val, ok := _userVerificationLevelToString[s]; ok {
		return val
	}
	return "unknown"
}

type ContactType uint8

const (
	ContactTypeEmail ContactType = iota + 1
	ContactTypePhone
)

var _userContactTypeToString = map[ContactType]string{
	ContactTypeEmail: "email",
	ContactTypePhone: "phone",
}

// NewUserContactType converts a stored uint8 to a UserContactType.
func NewUserContactType(t uint8) ContactType {
	return ContactType(t)
}

// NewUserContactTypeFromString returns UserContactType of the given string representation.
func NewUserContactTypeFromString(s string) ContactType {
	for userContactType, str := range _userContactTypeToString {
		if str == s {
			return userContactType
		}
	}
	return 0
}

// String returns the string representation of the UserContactType.
func (ct ContactType) String() string {
	if val, ok := _userContactTypeToString[ct]; ok {
		return val
	}
	return "unknown"
}

// Uint8 returns the uint8 representation of the UserContactType.
func (ct ContactType) Uint8() uint8 {
	return uint8(ct)
}

type Session struct {
	SessionID string
	User      User
}

type Profile struct {
	Locale       string
	ContactType  ContactType
	ContactValue string
}

type User struct {
	ID                   uint64
	Number               uint64
	VerificationLevel    VerificationLevel
	VerificationLevelMax bool
	Country              string
	Timezone             *string
	Locale               *string
	Status               Status
}

// ContextAuthenticatedSessionKey defines the request context key storing the authenticated session.
type ContextAuthenticatedSessionKey struct{}

// Service defines the interface for user-related operations, such as session validation.
type Service interface {
	ValidateSession(context.Context, string, bool) (Session, error)
}
