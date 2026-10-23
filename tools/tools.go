package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"time"

	"github.com/shopspring/decimal"
)

const (
	AcceptHeader              = "Accept"
	ContentTypeHeader         = "Content-Type"
	JSONContentType           = "application/json"
	FormUrlencodedContentType = "application/x-www-form-urlencoded"
)

const _charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// GenerateRandomString generates a random string of given length
func GenerateRandomString(length int) string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	result := make([]byte, length)
	for i := range result {
		result[i] = _charset[r.Intn(len(_charset))]
	}

	return string(result)
}

// GenerateTimeBasedString generates a string based on the current time
func GenerateTimeBasedString(length int) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))

	hashString := hex.EncodeToString(hash[:])

	if length > len(hashString) {
		length = len(hashString)
	}

	return hashString[:length]
}

// QuantizeAmount ensures the amount conforms to the asset scale and truncates any excess precision
func QuantizeAmount(amount decimal.Decimal, scale int32) (decimal.Decimal, error) {
	shifted := amount.Shift(scale)
	truncated := shifted.Truncate(0)
	if !shifted.Equal(truncated) {
		return decimal.Decimal{}, fmt.Errorf("[tools][QuantizeAmount][err: amount %s has more than %d decimal places]", amount.String(), scale)
	}
	return truncated, nil
}

// DecimalPtrFromString converts a string to a pointer to a decimal.Decimal, returning nil if the string is empty
func DecimalPtrFromString(raw string) (*decimal.Decimal, error) {
	if raw == "" {
		return nil, nil
	}
	v, err := decimal.NewFromString(raw)
	if err != nil {
		return nil, err
	}
	return &v, nil
}
