// Package validation provides security monitoring and suspicious activity logging
package validation

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	"unicode"
)

type SuspiciousActivityLogger struct {
	logger   *log.Logger
	mu       sync.RWMutex
	attempts map[string]*ActivityStats
	config   *SecurityConfig
}

// ActivityStats tracks suspicious activity per source
type ActivityStats struct {
	Count          int
	FirstSeen      time.Time
	LastSeen       time.Time
	Patterns       []string
	SuspicionLevel string // low, medium, high, critical
}

// SecurityConfig holds security monitoring configuration
type SecurityConfig struct {
	EnableLogging          bool
	LogSuspiciousPayloads  bool
	MaxSuspicionCount      int
	BlockAfterAttempts     int
	SuspicionResetDuration time.Duration
}

// DefaultSecurityConfig returns default security configuration
func DefaultSecurityConfig() *SecurityConfig {
	return &SecurityConfig{
		EnableLogging:          true,
		LogSuspiciousPayloads:  true,
		MaxSuspicionCount:      10,
		BlockAfterAttempts:     5,
		SuspicionResetDuration: 1 * time.Hour,
	}
}

// NewSuspiciousActivityLogger creates a new suspicious activity logger
func NewSuspiciousActivityLogger(logger *log.Logger, config *SecurityConfig) *SuspiciousActivityLogger {
	if config == nil {
		config = DefaultSecurityConfig()
	}
	if logger == nil {
		logger = log.Default()
	}
	return &SuspiciousActivityLogger{
		logger:   logger,
		attempts: make(map[string]*ActivityStats),
		config:   config,
	}
}

// LogSuspiciousInput logs a suspicious input attempt
func (sal *SuspiciousActivityLogger) LogSuspiciousInput(source, inputType, pattern, payload string, suspicionLevel string) {
	if !sal.config.EnableLogging {
		return
	}

	sal.mu.Lock()
	defer sal.mu.Unlock()

	// Get or create activity stats
	stats, exists := sal.attempts[source]
	if !exists {
		stats = &ActivityStats{
			FirstSeen:      time.Now(),
			Patterns:       make([]string, 0),
			SuspicionLevel: "low",
		}
		sal.attempts[source] = stats
	}

	// Update stats
	stats.Count++
	stats.LastSeen = time.Now()
	stats.Patterns = append(stats.Patterns, pattern)

	// Escalate suspicion level
	if stats.Count >= sal.config.BlockAfterAttempts {
		stats.SuspicionLevel = "critical"
	} else if stats.Count >= 3 {
		stats.SuspicionLevel = "high"
	} else if stats.Count >= 2 {
		stats.SuspicionLevel = "medium"
	}

	// Log the attempt
	logEntry := fmt.Sprintf(
		"[SECURITY] Suspicious %s detected | Source: %s | Pattern: %s | Level: %s | Count: %d/%d",
		inputType, source, pattern, stats.SuspicionLevel, stats.Count, sal.config.BlockAfterAttempts,
	)

	// Log payload if enabled and level is high
	if sal.config.LogSuspiciousPayloads && (suspicionLevel == "high" || suspicionLevel == "critical") {
		// Sanitize payload for logging
		sanitizedPayload := sanitizeForLogging(payload, 200)
		logEntry += fmt.Sprintf(" | Payload: %s", sanitizedPayload)
	}

	sal.logger.Println(logEntry)

	// Log to file for analysis
	if stats.SuspicionLevel == "critical" {
		sal.logger.Printf("[CRITICAL] Source %s has exceeded suspicious activity threshold. Consider blocking.", source)
	}
}

// ShouldBlock returns true if a source should be blocked
func (sal *SuspiciousActivityLogger) ShouldBlock(source string) bool {
	sal.mu.RLock()
	defer sal.mu.RUnlock()

	stats, exists := sal.attempts[source]
	if !exists {
		return false
	}

	// Block if exceeded attempts and within reset duration
	if stats.Count >= sal.config.BlockAfterAttempts {
		if time.Since(stats.FirstSeen) < sal.config.SuspicionResetDuration {
			return true
		}
	}

	return false
}

// GetActivityStats returns activity stats for a source
func (sal *SuspiciousActivityLogger) GetActivityStats(source string) *ActivityStats {
	sal.mu.RLock()
	defer sal.mu.RUnlock()
	return sal.attempts[source]
}

// ResetStats resets activity stats for a source
func (sal *SuspiciousActivityLogger) ResetStats(source string) {
	sal.mu.Lock()
	defer sal.mu.Unlock()
	delete(sal.attempts, source)
	sal.logger.Printf("[SECURITY] Reset activity stats for source: %s", source)
}

// GetAllStats returns all activity stats
func (sal *SuspiciousActivityLogger) GetAllStats() map[string]*ActivityStats {
	sal.mu.RLock()
	defer sal.mu.RUnlock()

	// Create a copy to avoid concurrent access issues
	statsCopy := make(map[string]*ActivityStats)
	for k, v := range sal.attempts {
		statsCopy[k] = &ActivityStats{
			Count:          v.Count,
			FirstSeen:      v.FirstSeen,
			LastSeen:       v.LastSeen,
			Patterns:       append([]string{}, v.Patterns...),
			SuspicionLevel: v.SuspicionLevel,
		}
	}
	return statsCopy
}

// DetectSuspiciousPatterns analyzes input for known attack patterns
func DetectSuspiciousPatterns(input string) []string {
	patterns := make([]string, 0)

	// SQL Injection patterns
	sqlPatterns := []string{
		"'", "\"", "--", "/*", "*/", ";",
		"DROP", "DELETE", "INSERT", "UPDATE", "SELECT",
		"UNION", "OR 1=1", "OR '1'='1", "' OR ''='",
		"exec(", "execute(", "xp_",
	}

	inputUpper := strings.ToUpper(input)
	for _, pattern := range sqlPatterns {
		if strings.Contains(inputUpper, strings.ToUpper(pattern)) {
			patterns = append(patterns, fmt.Sprintf("SQL_INJECTION:%s", pattern))
		}
	}

	// XSS patterns
	xssPatterns := []string{
		"<script", "</script>", "javascript:", "onerror=", "onload=",
		"<iframe", "onclick=", "onmouseover=", "alert(", "eval(",
	}

	inputLower := strings.ToLower(input)
	for _, pattern := range xssPatterns {
		if strings.Contains(inputLower, pattern) {
			patterns = append(patterns, fmt.Sprintf("XSS:%s", pattern))
		}
	}

	// Path traversal patterns
	pathPatterns := []string{
		"../", "..\\", "..", "%2e%2e", "..;", "..%00",
	}

	for _, pattern := range pathPatterns {
		if strings.Contains(input, pattern) {
			patterns = append(patterns, fmt.Sprintf("PATH_TRAVERSAL:%s", pattern))
		}
	}

	// Command injection patterns
	cmdPatterns := []string{
		"|", "&", ";", "`", "$(",
		"$(", "||", "&&", "\n", "\r",
	}

	for _, pattern := range cmdPatterns {
		if strings.Contains(input, pattern) {
			patterns = append(patterns, fmt.Sprintf("CMD_INJECTION:%s", pattern))
		}
	}

	// LDAP injection patterns
	ldapPatterns := []string{
		"*)", "*(", "*|", "*&", "*(|",
	}

	for _, pattern := range ldapPatterns {
		if strings.Contains(input, pattern) {
			patterns = append(patterns, fmt.Sprintf("LDAP_INJECTION:%s", pattern))
		}
	}

	// NoSQL injection patterns
	nosqlPatterns := []string{
		"$where", "$ne", "$gt", "$lt", "$regex",
		"{$", "[$", "||", "&&",
	}

	for _, pattern := range nosqlPatterns {
		if strings.Contains(input, pattern) {
			patterns = append(patterns, fmt.Sprintf("NOSQL_INJECTION:%s", pattern))
		}
	}

	// Check for excessive special characters (possible obfuscation)
	specialCharCount := 0
	for _, r := range input {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
			specialCharCount++
		}
	}

	if specialCharCount > len(input)/3 {
		patterns = append(patterns, "EXCESSIVE_SPECIAL_CHARS")
	}

	// Check for control characters
	for _, r := range input {
		if unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r' {
			patterns = append(patterns, "CONTROL_CHARACTERS")
			break
		}
	}

	// Check for null bytes
	if strings.Contains(input, "\x00") {
		patterns = append(patterns, "NULL_BYTE_INJECTION")
	}

	// Check for encoding attempts
	if strings.Contains(input, "%") && len(input) > 10 {
		// Multiple percent signs suggest URL encoding abuse
		percentCount := strings.Count(input, "%")
		if percentCount > 5 {
			patterns = append(patterns, "URL_ENCODING_ABUSE")
		}
	}

	return patterns
}

// sanitizeForLogging sanitizes a payload for safe logging
func sanitizeForLogging(payload string, maxLen int) string {
	// Remove control characters
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, payload)

	// Truncate if too long
	if len(cleaned) > maxLen {
		cleaned = cleaned[:maxLen] + "...[truncated]"
	}

	// Escape quotes for JSON logging
	cleaned = strings.ReplaceAll(cleaned, "\"", "\\\"")

	return cleaned
}

// EnhancedInputValidator extends InputValidator with additional security
type EnhancedInputValidator struct {
	*InputValidator
	activityLogger *SuspiciousActivityLogger
}

// NewEnhancedInputValidator creates a validator with suspicious activity logging
func NewEnhancedInputValidator(config *ValidationConfig, secConfig *SecurityConfig, logger *log.Logger) *EnhancedInputValidator {
	return &EnhancedInputValidator{
		InputValidator: NewInputValidator(config),
		activityLogger: NewSuspiciousActivityLogger(logger, secConfig),
	}
}

// ValidateWithSecurityChecks validates input and logs suspicious patterns
func (eiv *EnhancedInputValidator) ValidateWithSecurityChecks(input string, inputType, source string) error {
	// Always check if source should be blocked first
	if eiv.activityLogger.ShouldBlock(source) {
		return fmt.Errorf("source %s is blocked due to excessive suspicious activity", source)
	}

	// Detect suspicious patterns
	patterns := DetectSuspiciousPatterns(input)

	if len(patterns) > 0 {
		// Calculate suspicion level
		level := "low"
		if len(patterns) >= 5 {
			level = "critical"
		} else if len(patterns) >= 3 {
			level = "high"
		} else if len(patterns) >= 2 {
			level = "medium"
		}

		// Log suspicious activity
		patternStr := strings.Join(patterns, ", ")
		eiv.activityLogger.LogSuspiciousInput(source, inputType, patternStr, input, level)

		// Check again if source should be blocked (may have just crossed threshold)
		if eiv.activityLogger.ShouldBlock(source) {
			return fmt.Errorf("source %s is blocked due to excessive suspicious activity", source)
		}

		// Return error for critical patterns
		if level == "critical" {
			return fmt.Errorf("input contains multiple suspicious patterns: %s", patternStr)
		}
	}

	return nil
}

// ValidateClientIDWithSecurity validates client ID with security checks
func (eiv *EnhancedInputValidator) ValidateClientIDWithSecurity(clientID, source string) error {
	// Check for suspicious patterns first
	if err := eiv.ValidateWithSecurityChecks(clientID, "CLIENT_ID", source); err != nil {
		return err
	}

	// Then do normal validation
	return eiv.ValidateClientID(clientID)
}

// ValidateSymbolWithSecurity validates symbol with security checks
func (eiv *EnhancedInputValidator) ValidateSymbolWithSecurity(symbol, source string) error {
	// Check for suspicious patterns first
	if err := eiv.ValidateWithSecurityChecks(symbol, "SYMBOL", source); err != nil {
		return err
	}

	// Then do normal validation
	return eiv.ValidateSymbol(symbol)
}

// ValidateOrderRequestWithSecurity validates order with comprehensive security
func (eiv *EnhancedInputValidator) ValidateOrderRequestWithSecurity(req *OrderRequest, source string) error {
	// Check each field for suspicious patterns
	if err := eiv.ValidateClientIDWithSecurity(req.ClientID, source); err != nil {
		return err
	}

	if err := eiv.ValidateSymbolWithSecurity(req.Symbol, source); err != nil {
		return err
	}

	// Validate side with security check
	if err := eiv.ValidateWithSecurityChecks(req.Side, "SIDE", source); err != nil {
		return err
	}

	// Then do normal validation
	return eiv.ValidateOrderRequest(req)
}

// GetActivityLogger returns the activity logger for monitoring
func (eiv *EnhancedInputValidator) GetActivityLogger() *SuspiciousActivityLogger {
	return eiv.activityLogger
}

// SecurityReport generates a security report
type SecurityReport struct {
	Timestamp       time.Time                 `json:"timestamp"`
	TotalSources    int                       `json:"total_sources"`
	CriticalSources int                       `json:"critical_sources"`
	HighRiskSources int                       `json:"high_risk_sources"`
	BlockedSources  int                       `json:"blocked_sources"`
	TopPatterns     map[string]int            `json:"top_patterns"`
	SourceDetails   map[string]*ActivityStats `json:"source_details"`
}

// GenerateSecurityReport generates a comprehensive security report
func (eiv *EnhancedInputValidator) GenerateSecurityReport() *SecurityReport {
	allStats := eiv.activityLogger.GetAllStats()

	report := &SecurityReport{
		Timestamp:     time.Now(),
		TotalSources:  len(allStats),
		TopPatterns:   make(map[string]int),
		SourceDetails: allStats,
	}

	// Analyze stats
	for source, stats := range allStats {
		switch stats.SuspicionLevel {
		case "critical":
			report.CriticalSources++
		case "high":
			report.HighRiskSources++
		}

		if eiv.activityLogger.ShouldBlock(source) {
			report.BlockedSources++
		}

		// Count patterns
		for _, pattern := range stats.Patterns {
			report.TopPatterns[pattern]++
		}
	}

	return report
}

// ExportSecurityLog exports security logs as JSON
func (eiv *EnhancedInputValidator) ExportSecurityLog() (string, error) {
	report := eiv.GenerateSecurityReport()
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal security report: %w", err)
	}
	return string(data), nil
}
