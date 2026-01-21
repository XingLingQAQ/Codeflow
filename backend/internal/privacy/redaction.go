// Package privacy - PII Redaction implementation
package privacy

import (
	"context"
	"regexp"
	"strings"
	"sync"
)

// PIIType represents the type of PII detected
type PIIType string

const (
	PIITypeAPIKey       PIIType = "api_key"
	PIITypePassword     PIIType = "password"
	PIITypeEmail        PIIType = "email"
	PIITypePhone        PIIType = "phone"
	PIITypeCreditCard   PIIType = "credit_card"
	PIITypeSSN          PIIType = "ssn"
	PIITypeIPAddress    PIIType = "ip_address"
	PIITypeJWT          PIIType = "jwt"
	PIITypePrivateKey   PIIType = "private_key"
	PIITypeAWSKey       PIIType = "aws_key"
	PIITypeGitHubToken  PIIType = "github_token"
	PIITypeSlackToken   PIIType = "slack_token"
	PIITypeGenericToken PIIType = "generic_token"
)

// PIIMatch represents a detected PII match
type PIIMatch struct {
	Type       PIIType `json:"type"`
	Value      string  `json:"value"`
	StartIndex int     `json:"start_index"`
	EndIndex   int     `json:"end_index"`
	Confidence float64 `json:"confidence"`
}

// RedactionResult represents the result of PII redaction
type RedactionResult struct {
	OriginalText  string     `json:"original_text,omitempty"`
	RedactedText  string     `json:"redacted_text"`
	Matches       []PIIMatch `json:"matches"`
	RedactedCount int        `json:"redacted_count"`
}

// RedactionConfig configures PII redaction behavior
type RedactionConfig struct {
	EnabledTypes     []PIIType `json:"enabled_types"`
	RedactionMask    string    `json:"redaction_mask"`
	PreserveLength   bool      `json:"preserve_length"`
	IncludeOriginal  bool      `json:"include_original"`
	CustomPatterns   map[string]*regexp.Regexp
	MinConfidence    float64 `json:"min_confidence"`
}

// DefaultRedactionConfig returns default redaction configuration
var DefaultRedactionConfig = RedactionConfig{
	EnabledTypes: []PIIType{
		PIITypeAPIKey, PIITypePassword, PIITypeEmail, PIITypePhone,
		PIITypeCreditCard, PIITypeSSN, PIITypeIPAddress, PIITypeJWT,
		PIITypePrivateKey, PIITypeAWSKey, PIITypeGitHubToken, PIITypeSlackToken,
		PIITypeGenericToken,
	},
	RedactionMask:   "[REDACTED]",
	PreserveLength:  false,
	IncludeOriginal: false,
	MinConfidence:   0.7,
}

// PIIRedactor handles PII detection and redaction
type PIIRedactor struct {
	config   RedactionConfig
	patterns map[PIIType]*regexp.Regexp
	mu       sync.RWMutex
}

// NewPIIRedactor creates a new PII redactor
func NewPIIRedactor(config *RedactionConfig) *PIIRedactor {
	cfg := DefaultRedactionConfig
	if config != nil {
		if len(config.EnabledTypes) > 0 {
			cfg.EnabledTypes = config.EnabledTypes
		}
		if config.RedactionMask != "" {
			cfg.RedactionMask = config.RedactionMask
		}
		cfg.PreserveLength = config.PreserveLength
		cfg.IncludeOriginal = config.IncludeOriginal
		if config.MinConfidence > 0 {
			cfg.MinConfidence = config.MinConfidence
		}
		if config.CustomPatterns != nil {
			cfg.CustomPatterns = config.CustomPatterns
		}
	}

	r := &PIIRedactor{
		config:   cfg,
		patterns: make(map[PIIType]*regexp.Regexp),
	}
	r.initPatterns()
	return r
}

// initPatterns initializes regex patterns for PII detection
func (r *PIIRedactor) initPatterns() {
	// API Keys (generic patterns)
	r.patterns[PIITypeAPIKey] = regexp.MustCompile(`(?i)(api[_-]?key|apikey|api_secret|api_token)[=:\s]+['"]?([a-zA-Z0-9_\-]{20,})['"]?`)

	// Passwords in config/code
	r.patterns[PIITypePassword] = regexp.MustCompile(`(?i)(password|passwd|pwd|secret)[=:\s]+['"]?([^\s'"]{8,})['"]?`)

	// Email addresses
	r.patterns[PIITypeEmail] = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)

	// Phone numbers (various formats)
	r.patterns[PIITypePhone] = regexp.MustCompile(`(?:\+?1[-.\s]?)?\(?[0-9]{3}\)?[-.\s]?[0-9]{3}[-.\s]?[0-9]{4}`)

	// Credit card numbers
	r.patterns[PIITypeCreditCard] = regexp.MustCompile(`\b(?:4[0-9]{12}(?:[0-9]{3})?|5[1-5][0-9]{14}|3[47][0-9]{13}|6(?:011|5[0-9]{2})[0-9]{12})\b`)

	// SSN (US Social Security Number)
	r.patterns[PIITypeSSN] = regexp.MustCompile(`\b[0-9]{3}[-\s]?[0-9]{2}[-\s]?[0-9]{4}\b`)

	// IP Addresses (IPv4)
	r.patterns[PIITypeIPAddress] = regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\b`)

	// JWT tokens
	r.patterns[PIITypeJWT] = regexp.MustCompile(`eyJ[a-zA-Z0-9_-]*\.eyJ[a-zA-Z0-9_-]*\.[a-zA-Z0-9_-]*`)

	// Private keys (PEM format)
	r.patterns[PIITypePrivateKey] = regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----[\s\S]*?-----END (?:RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----`)

	// AWS Access Key ID
	r.patterns[PIITypeAWSKey] = regexp.MustCompile(`(?:AKIA|ABIA|ACCA|ASIA)[0-9A-Z]{16}`)

	// GitHub tokens
	r.patterns[PIITypeGitHubToken] = regexp.MustCompile(`(?:ghp|gho|ghu|ghs|ghr)_[a-zA-Z0-9]{36,}`)

	// Slack tokens
	r.patterns[PIITypeSlackToken] = regexp.MustCompile(`xox[baprs]-[0-9]{10,13}-[0-9]{10,13}-[a-zA-Z0-9]{24}`)

	// Generic tokens/secrets
	r.patterns[PIITypeGenericToken] = regexp.MustCompile(`(?i)(token|secret|bearer|auth)[=:\s]+['"]?([a-zA-Z0-9_\-]{20,})['"]?`)

	// Add custom patterns
	if r.config.CustomPatterns != nil {
		for name, pattern := range r.config.CustomPatterns {
			r.patterns[PIIType(name)] = pattern
		}
	}
}

// Detect detects PII in the given text
func (r *PIIRedactor) Detect(_ context.Context, text string) []PIIMatch {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var matches []PIIMatch

	for _, piiType := range r.config.EnabledTypes {
		pattern, ok := r.patterns[piiType]
		if !ok {
			continue
		}

		found := pattern.FindAllStringSubmatchIndex(text, -1)
		for _, loc := range found {
			if len(loc) < 2 {
				continue
			}

			startIdx := loc[0]
			endIdx := loc[1]
			value := text[startIdx:endIdx]

			// Calculate confidence based on pattern type
			confidence := r.calculateConfidence(piiType, value)
			if confidence < r.config.MinConfidence {
				continue
			}

			matches = append(matches, PIIMatch{
				Type:       piiType,
				Value:      value,
				StartIndex: startIdx,
				EndIndex:   endIdx,
				Confidence: confidence,
			})
		}
	}

	// Sort by start index and remove overlaps
	matches = r.removeOverlaps(matches)
	return matches
}

// Redact redacts PII from the given text
func (r *PIIRedactor) Redact(ctx context.Context, text string) *RedactionResult {
	matches := r.Detect(ctx, text)

	result := &RedactionResult{
		Matches:       matches,
		RedactedCount: len(matches),
	}

	if r.config.IncludeOriginal {
		result.OriginalText = text
	}

	if len(matches) == 0 {
		result.RedactedText = text
		return result
	}

	// Build redacted text
	var builder strings.Builder
	lastEnd := 0

	for _, match := range matches {
		// Add text before this match
		if match.StartIndex > lastEnd {
			builder.WriteString(text[lastEnd:match.StartIndex])
		}

		// Add redaction mask
		if r.config.PreserveLength {
			maskLen := match.EndIndex - match.StartIndex
			builder.WriteString(strings.Repeat("*", maskLen))
		} else {
			builder.WriteString(r.config.RedactionMask)
		}

		lastEnd = match.EndIndex
	}

	// Add remaining text
	if lastEnd < len(text) {
		builder.WriteString(text[lastEnd:])
	}

	result.RedactedText = builder.String()
	return result
}

// calculateConfidence calculates confidence score for a match
func (r *PIIRedactor) calculateConfidence(piiType PIIType, value string) float64 {
	// Base confidence
	confidence := 0.8

	switch piiType {
	case PIITypeEmail:
		// Higher confidence for well-formed emails
		if strings.Contains(value, "@") && strings.Contains(value, ".") {
			confidence = 0.95
		}
	case PIITypeCreditCard:
		// Validate Luhn algorithm for credit cards
		if r.validateLuhn(value) {
			confidence = 0.98
		} else {
			confidence = 0.5
		}
	case PIITypeJWT:
		// JWT has very specific format
		confidence = 0.99
	case PIITypePrivateKey:
		// Private keys are very distinctive
		confidence = 0.99
	case PIITypeAWSKey, PIITypeGitHubToken, PIITypeSlackToken:
		// Platform-specific tokens have distinctive prefixes
		confidence = 0.95
	case PIITypePassword, PIITypeAPIKey, PIITypeGenericToken:
		// Context-dependent, lower confidence
		confidence = 0.75
	case PIITypePhone:
		// Phone numbers can have false positives
		confidence = 0.7
	case PIITypeSSN:
		// SSN pattern can match other numbers
		confidence = 0.7
	case PIITypeIPAddress:
		// IP addresses are distinctive
		confidence = 0.85
	}

	return confidence
}

// validateLuhn validates a credit card number using Luhn algorithm
func (r *PIIRedactor) validateLuhn(number string) bool {
	// Remove non-digits
	var digits []int
	for _, c := range number {
		if c >= '0' && c <= '9' {
			digits = append(digits, int(c-'0'))
		}
	}

	if len(digits) < 13 || len(digits) > 19 {
		return false
	}

	sum := 0
	alternate := false

	for i := len(digits) - 1; i >= 0; i-- {
		d := digits[i]
		if alternate {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		alternate = !alternate
	}

	return sum%10 == 0
}

// removeOverlaps removes overlapping matches, keeping the more specific ones
func (r *PIIRedactor) removeOverlaps(matches []PIIMatch) []PIIMatch {
	if len(matches) <= 1 {
		return matches
	}

	// Sort by start index
	for i := 0; i < len(matches)-1; i++ {
		for j := i + 1; j < len(matches); j++ {
			if matches[j].StartIndex < matches[i].StartIndex {
				matches[i], matches[j] = matches[j], matches[i]
			}
		}
	}

	// Remove overlaps - prefer more specific types over generic ones
	var result []PIIMatch
	for _, match := range matches {
		if len(result) == 0 {
			result = append(result, match)
			continue
		}

		last := &result[len(result)-1]
		if match.StartIndex >= last.EndIndex {
			// No overlap
			result = append(result, match)
		} else {
			// Overlap detected - prefer more specific type
			// Generic types are less specific
			lastIsGeneric := last.Type == PIITypeGenericToken || last.Type == PIITypeAPIKey || last.Type == PIITypePassword
			matchIsGeneric := match.Type == PIITypeGenericToken || match.Type == PIITypeAPIKey || match.Type == PIITypePassword

			if lastIsGeneric && !matchIsGeneric {
				// Replace generic with specific
				result[len(result)-1] = match
			} else if !lastIsGeneric && matchIsGeneric {
				// Keep specific, ignore generic
				continue
			} else if match.Confidence > last.Confidence {
				// Same specificity, prefer higher confidence
				result[len(result)-1] = match
			} else if match.EndIndex-match.StartIndex > last.EndIndex-last.StartIndex && match.Confidence >= last.Confidence {
				// Same specificity and confidence, prefer longer match
				result[len(result)-1] = match
			}
			// Otherwise, keep the existing match
		}
	}

	return result
}

// AddCustomPattern adds a custom PII pattern
func (r *PIIRedactor) AddCustomPattern(name string, pattern *regexp.Regexp) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.patterns[PIIType(name)] = pattern
	r.config.EnabledTypes = append(r.config.EnabledTypes, PIIType(name))
}

// SetEnabledTypes sets which PII types to detect
func (r *PIIRedactor) SetEnabledTypes(types []PIIType) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.config.EnabledTypes = types
}

// GetEnabledTypes returns currently enabled PII types
func (r *PIIRedactor) GetEnabledTypes() []PIIType {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.config.EnabledTypes
}
