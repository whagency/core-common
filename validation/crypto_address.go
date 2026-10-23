package validation

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"math/big"
	"strings"
)

var base58Alphabet = []byte("123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz")
var cryptoAddressValidators = map[string]func(string) error{
	"trx": validateTRONAddress,
}

// ValidateCryptoAddress verifies that an address matches the expected format for the provided blockchain network.
func ValidateCryptoAddress(network string, address string) error {
	normalizedNetwork := strings.ToLower(strings.TrimSpace(network))
	validate, ok := cryptoAddressValidators[normalizedNetwork]
	if !ok {
		return fmt.Errorf("[validation][ValidateCryptoAddress][network:%s][err: unsupported network]", network)
	}

	return validate(strings.TrimSpace(address))
}

// RegisterCryptoAddressValidator registers a validator for a blockchain network.
func RegisterCryptoAddressValidator(network string, validate func(string) error) {
	cryptoAddressValidators[strings.ToLower(strings.TrimSpace(network))] = validate
}

// validateTRONAddress validates a TRON address.
func validateTRONAddress(address string) error {
	decoded, err := decodeBase58(address)
	if err != nil {
		return fmt.Errorf("[validation][ValidateCryptoAddress][validateTRONAddress][err: %w]", err)
	}

	if len(decoded) != 25 {
		return fmt.Errorf("[validation][ValidateCryptoAddress][validateTRONAddress][err: unexpected decoded length]")
	}

	if decoded[0] != 0x41 {
		return fmt.Errorf("[validation][ValidateCryptoAddress][validateTRONAddress][err: invalid address prefix]")
	}

	payload := decoded[:21]
	checksum := decoded[21:]

	first := sha256.Sum256(payload)
	second := sha256.Sum256(first[:])

	if !bytes.Equal(checksum, second[:4]) {
		return fmt.Errorf("[validation][ValidateCryptoAddress][validateTRONAddress][err: invalid checksum]")
	}

	return nil
}

// decodeBase58 decodes a Base58-encoded string into a byte slice. It returns an error if the input contains invalid characters.
func decodeBase58(input string) ([]byte, error) {
	result := big.NewInt(0)
	base := big.NewInt(58)

	for i := 0; i < len(input); i++ {
		digit := indexByte(base58Alphabet, input[i])
		if digit < 0 {
			return nil, fmt.Errorf("[decodeBase58][err: invalid character]")
		}

		result.Mul(result, base)
		result.Add(result, big.NewInt(int64(digit)))
	}

	decoded := result.Bytes()

	leadingZeros := 0
	for leadingZeros < len(input) && input[leadingZeros] == '1' {
		leadingZeros++
	}

	output := make([]byte, leadingZeros+len(decoded))
	copy(output[leadingZeros:], decoded)

	return output, nil
}

// indexByte returns the index of the first occurrence of target in data, or -1 if target is not present in data.
func indexByte(data []byte, target byte) int {
	for i := 0; i < len(data); i++ {
		if data[i] == target {
			return i
		}
	}
	return -1
}
