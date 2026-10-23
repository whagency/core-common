package validation

import "testing"

func TestPasswordPolicyValidate(t *testing.T) {
	policy := NewDefaultPasswordPolicy(8)

	if err := policy.Validate("Aa1!aaaa"); err != nil {
		t.Fatalf("expected valid password, got error: %v", err)
	}

	if err := policy.Validate("weak"); err == nil {
		t.Fatal("expected weak password to fail")
	}
}

func TestValidateCryptoAddress(t *testing.T) {
	if err := ValidateCryptoAddress("trx", "TJRabPrwbZy45sbavfcjinPJC18kjpRTv8"); err != nil {
		t.Fatalf("expected valid trx address, got error: %v", err)
	}

	if err := ValidateCryptoAddress("trx", "invalid"); err == nil {
		t.Fatal("expected invalid trx address to fail")
	}
}
