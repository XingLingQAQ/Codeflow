// Package privacy - PII Redaction tests
package privacy

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

func TestPIIRedactor_DetectEmail(t *testing.T) {
	redactor := NewPIIRedactor(nil)
	ctx := context.Background()

	text := "Contact us at support@example.com or sales@company.org"
	matches := redactor.Detect(ctx, text)

	emailCount := 0
	for _, m := range matches {
		if m.Type == PIITypeEmail {
			emailCount++
		}
	}

	if emailCount != 2 {
		t.Errorf("expected 2 email matches, got %d", emailCount)
	}
}

func TestPIIRedactor_DetectAPIKey(t *testing.T) {
	redactor := NewPIIRedactor(nil)
	ctx := context.Background()

	text := `config:
  api_key: sk_live_abcdefghijklmnopqrstuvwxyz123456
  api_secret = "secret_key_1234567890abcdefghij"`

	matches := redactor.Detect(ctx, text)

	if len(matches) == 0 {
		t.Error("expected to detect API keys")
	}

	foundAPIKey := false
	for _, m := range matches {
		if m.Type == PIITypeAPIKey || m.Type == PIITypeGenericToken {
			foundAPIKey = true
			break
		}
	}

	if !foundAPIKey {
		t.Error("expected to find API key match")
	}
}

func TestPIIRedactor_DetectJWT(t *testing.T) {
	redactor := NewPIIRedactor(nil)
	ctx := context.Background()

	// Sample JWT token
	text := "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"

	matches := redactor.Detect(ctx, text)

	foundJWT := false
	for _, m := range matches {
		if m.Type == PIITypeJWT {
			foundJWT = true
			if m.Confidence < 0.9 {
				t.Errorf("expected high confidence for JWT, got %f", m.Confidence)
			}
			break
		}
	}

	if !foundJWT {
		t.Error("expected to find JWT match")
	}
}

func TestPIIRedactor_DetectAWSKey(t *testing.T) {
	redactor := NewPIIRedactor(nil)
	ctx := context.Background()

	text := "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE"

	matches := redactor.Detect(ctx, text)

	foundAWS := false
	for _, m := range matches {
		if m.Type == PIITypeAWSKey {
			foundAWS = true
			break
		}
	}

	if !foundAWS {
		t.Error("expected to find AWS key match")
	}
}

func TestPIIRedactor_DetectGitHubToken(t *testing.T) {
	redactor := NewPIIRedactor(nil)
	ctx := context.Background()

	// GitHub token needs at least 36 characters after prefix
	text := "GITHUB_TOKEN=ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"

	matches := redactor.Detect(ctx, text)

	foundGH := false
	for _, m := range matches {
		if m.Type == PIITypeGitHubToken {
			foundGH = true
			break
		}
	}

	if !foundGH {
		t.Error("expected to find GitHub token match")
	}
}

func TestPIIRedactor_DetectPrivateKey(t *testing.T) {
	redactor := NewPIIRedactor(nil)
	ctx := context.Background()

	text := `-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA0Z3VS5JJcds3xfn/ygWyF8PbnGy
-----END RSA PRIVATE KEY-----`

	matches := redactor.Detect(ctx, text)

	foundKey := false
	for _, m := range matches {
		if m.Type == PIITypePrivateKey {
			foundKey = true
			if m.Confidence < 0.9 {
				t.Errorf("expected high confidence for private key, got %f", m.Confidence)
			}
			break
		}
	}

	if !foundKey {
		t.Error("expected to find private key match")
	}
}

func TestPIIRedactor_DetectCreditCard(t *testing.T) {
	redactor := NewPIIRedactor(nil)
	ctx := context.Background()

	// Valid Visa test number
	text := "Card number: 4111111111111111"

	matches := redactor.Detect(ctx, text)

	foundCC := false
	for _, m := range matches {
		if m.Type == PIITypeCreditCard {
			foundCC = true
			// Luhn validation should give high confidence
			if m.Confidence < 0.9 {
				t.Errorf("expected high confidence for valid credit card, got %f", m.Confidence)
			}
			break
		}
	}

	if !foundCC {
		t.Error("expected to find credit card match")
	}
}

func TestPIIRedactor_DetectIPAddress(t *testing.T) {
	redactor := NewPIIRedactor(nil)
	ctx := context.Background()

	text := "Server IP: 192.168.1.100, Gateway: 10.0.0.1"

	matches := redactor.Detect(ctx, text)

	ipCount := 0
	for _, m := range matches {
		if m.Type == PIITypeIPAddress {
			ipCount++
		}
	}

	if ipCount != 2 {
		t.Errorf("expected 2 IP address matches, got %d", ipCount)
	}
}

func TestPIIRedactor_Redact(t *testing.T) {
	redactor := NewPIIRedactor(nil)
	ctx := context.Background()

	text := "Contact: user@example.com, API: api_key=sk_test_1234567890abcdefghij"

	result := redactor.Redact(ctx, text)

	if result.RedactedCount == 0 {
		t.Error("expected some redactions")
	}

	if result.RedactedText == text {
		t.Error("expected redacted text to be different from original")
	}

	// Check that email is redacted
	if strings.Contains(result.RedactedText, "@example.com") {
		t.Error("expected email to be redacted")
	}
}

func TestPIIRedactor_RedactPreserveLength(t *testing.T) {
	config := &RedactionConfig{
		EnabledTypes:   []PIIType{PIITypeEmail},
		PreserveLength: true,
	}
	redactor := NewPIIRedactor(config)
	ctx := context.Background()

	text := "Email: test@example.com"
	result := redactor.Redact(ctx, text)

	// With preserve length, the redacted text should have same length
	if len(result.RedactedText) != len(text) {
		t.Errorf("expected same length, got original=%d, redacted=%d", len(text), len(result.RedactedText))
	}
}

func TestPIIRedactor_CustomPattern(t *testing.T) {
	redactor := NewPIIRedactor(nil)
	ctx := context.Background()

	// Add custom pattern for internal IDs
	pattern := regexp.MustCompile(`INTERNAL-[A-Z0-9]{8}`)
	redactor.AddCustomPattern("internal_id", pattern)

	text := "Reference: INTERNAL-ABC12345"
	matches := redactor.Detect(ctx, text)

	foundCustom := false
	for _, m := range matches {
		if m.Type == "internal_id" {
			foundCustom = true
			break
		}
	}

	if !foundCustom {
		t.Error("expected to find custom pattern match")
	}
}

func TestPIIRedactor_SetEnabledTypes(t *testing.T) {
	redactor := NewPIIRedactor(nil)
	ctx := context.Background()

	// Only enable email detection
	redactor.SetEnabledTypes([]PIIType{PIITypeEmail})

	text := "Email: test@example.com, IP: 192.168.1.1"
	matches := redactor.Detect(ctx, text)

	for _, m := range matches {
		if m.Type == PIITypeIPAddress {
			t.Error("IP address should not be detected when disabled")
		}
	}

	foundEmail := false
	for _, m := range matches {
		if m.Type == PIITypeEmail {
			foundEmail = true
			break
		}
	}

	if !foundEmail {
		t.Error("expected to find email match")
	}
}

func TestPIIRedactor_NoMatches(t *testing.T) {
	redactor := NewPIIRedactor(nil)
	ctx := context.Background()

	text := "This is a normal text without any PII"
	result := redactor.Redact(ctx, text)

	if result.RedactedCount != 0 {
		t.Errorf("expected 0 redactions, got %d", result.RedactedCount)
	}

	if result.RedactedText != text {
		t.Error("expected redacted text to be same as original when no PII found")
	}
}

func TestPIIRedactor_OverlappingMatches(t *testing.T) {
	redactor := NewPIIRedactor(nil)
	ctx := context.Background()

	// Text with potentially overlapping patterns
	text := "token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"

	result := redactor.Redact(ctx, text)

	// Should handle overlapping matches gracefully
	if result.RedactedText == "" {
		t.Error("redacted text should not be empty")
	}
}

func TestLuhnValidation(t *testing.T) {
	redactor := NewPIIRedactor(nil)

	// Valid test card numbers
	validCards := []string{
		"4111111111111111", // Visa
		"5500000000000004", // Mastercard
		"340000000000009",  // Amex
	}

	for _, card := range validCards {
		if !redactor.validateLuhn(card) {
			t.Errorf("expected %s to be valid", card)
		}
	}

	// Invalid card numbers
	invalidCards := []string{
		"4111111111111112",
		"1234567890123456",
	}

	for _, card := range invalidCards {
		if redactor.validateLuhn(card) {
			t.Errorf("expected %s to be invalid", card)
		}
	}
}
