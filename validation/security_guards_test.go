package validation

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

// TestMaliciousSQLInjectionAttempts tests SQL injection detection
func TestMaliciousSQLInjectionAttempts(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string // Expected patterns to be detected
	}{
		{
			name:     "classic SQL injection",
			input:    "admin' OR '1'='1",
			expected: []string{"SQL_INJECTION"},
		},
		{
			name:     "DROP TABLE attempt",
			input:    "client'; DROP TABLE orders;--",
			expected: []string{"SQL_INJECTION"},
		},
		{
			name:     "UNION SELECT attack",
			input:    "' UNION SELECT * FROM users--",
			expected: []string{"SQL_INJECTION"},
		},
		{
			name:     "comment-based injection",
			input:    "admin'--",
			expected: []string{"SQL_INJECTION"},
		},
		{
			name:     "boolean-based blind injection",
			input:    "1' AND '1'='1",
			expected: []string{"SQL_INJECTION"},
		},
		{
			name:     "time-based blind injection",
			input:    "'; WAITFOR DELAY '00:00:05'--",
			expected: []string{"SQL_INJECTION"},
		},
		{
			name:     "stacked queries",
			input:    "'; DELETE FROM users WHERE 1=1;--",
			expected: []string{"SQL_INJECTION"},
		},
		{
			name:     "stored procedure execution",
			input:    "'; EXEC xp_cmdshell('dir')--",
			expected: []string{"SQL_INJECTION"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patterns := DetectSuspiciousPatterns(tt.input)
			
			// Check if expected patterns are detected
			for _, expected := range tt.expected {
				found := false
				for _, pattern := range patterns {
					if strings.Contains(pattern, expected) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected pattern %s not detected in input: %s", expected, tt.input)
				}
			}

			if len(patterns) == 0 {
				t.Errorf("No suspicious patterns detected for SQL injection: %s", tt.input)
			}
		})
	}
}

// TestMaliciousXSSAttempts tests XSS detection
func TestMaliciousXSSAttempts(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "script tag injection",
			input: "<script>alert('XSS')</script>",
		},
		{
			name:  "javascript protocol",
			input: "javascript:alert(document.cookie)",
		},
		{
			name:  "onerror event",
			input: "<img src=x onerror=alert('XSS')>",
		},
		{
			name:  "onload event",
			input: "<body onload=alert('XSS')>",
		},
		{
			name:  "iframe injection",
			input: "<iframe src='http://evil.com'></iframe>",
		},
		{
			name:  "onclick event",
			input: "<div onclick='malicious()'>Click</div>",
		},
		{
			name:  "eval injection",
			input: "eval('malicious code')",
		},
		{
			name:  "onmouseover event",
			input: "<span onmouseover='alert(1)'>hover</span>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patterns := DetectSuspiciousPatterns(tt.input)
			
			// Should detect XSS patterns
			hasXSS := false
			for _, pattern := range patterns {
				if strings.Contains(pattern, "XSS") {
					hasXSS = true
					break
				}
			}

			if !hasXSS {
				t.Errorf("XSS pattern not detected in: %s", tt.input)
			}
		})
	}
}

// TestMaliciousPathTraversalAttempts tests path traversal detection
func TestMaliciousPathTraversalAttempts(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "basic path traversal",
			input: "../../../etc/passwd",
		},
		{
			name:  "windows path traversal",
			input: "..\\..\\..\\windows\\system32",
		},
		{
			name:  "URL encoded traversal",
			input: "%2e%2e%2f%2e%2e%2f",
		},
		{
			name:  "null byte injection",
			input: "../../../etc/passwd%00",
		},
		{
			name:  "mixed separators",
			input: "..;/..;/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patterns := DetectSuspiciousPatterns(tt.input)
			
			hasPathTraversal := false
			for _, pattern := range patterns {
				if strings.Contains(pattern, "PATH_TRAVERSAL") {
					hasPathTraversal = true
					break
				}
			}

			if !hasPathTraversal {
				t.Errorf("Path traversal not detected in: %s", tt.input)
			}
		})
	}
}

// TestMaliciousCommandInjectionAttempts tests command injection detection
func TestMaliciousCommandInjectionAttempts(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "pipe command",
			input: "input | cat /etc/passwd",
		},
		{
			name:  "semicolon separator",
			input: "input; rm -rf /",
		},
		{
			name:  "ampersand separator",
			input: "input & whoami",
		},
		{
			name:  "backtick command substitution",
			input: "input`whoami`",
		},
		{
			name:  "dollar paren substitution",
			input: "input$(whoami)",
		},
		{
			name:  "or operator",
			input: "input || ls",
		},
		{
			name:  "and operator",
			input: "input && pwd",
		},
		{
			name:  "newline injection",
			input: "input\nrm file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patterns := DetectSuspiciousPatterns(tt.input)
			
			hasCmdInjection := false
			for _, pattern := range patterns {
				if strings.Contains(pattern, "CMD_INJECTION") {
					hasCmdInjection = true
					break
				}
			}

			if !hasCmdInjection {
				t.Errorf("Command injection not detected in: %s", tt.input)
			}
		})
	}
}

// TestMaliciousLDAPInjectionAttempts tests LDAP injection detection
func TestMaliciousLDAPInjectionAttempts(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "LDAP wildcard injection",
			input: "*)",
		},
		{
			name:  "LDAP OR filter",
			input: "*(|(uid=*))",
		},
		{
			name:  "LDAP AND filter",
			input: "*(&(uid=*))",
		},
		{
			name:  "LDAP filter bypass",
			input: "admin*)|",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patterns := DetectSuspiciousPatterns(tt.input)
			
			hasLDAPInjection := false
			for _, pattern := range patterns {
				if strings.Contains(pattern, "LDAP_INJECTION") {
					hasLDAPInjection = true
					break
				}
			}

			if !hasLDAPInjection {
				t.Errorf("LDAP injection not detected in: %s", tt.input)
			}
		})
	}
}

// TestMaliciousNoSQLInjectionAttempts tests NoSQL injection detection
func TestMaliciousNoSQLInjectionAttempts(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "MongoDB $where injection",
			input: "{$where: 'this.password.length > 0'}",
		},
		{
			name:  "MongoDB $ne operator",
			input: "{username: 'admin', password: {$ne: 1}}",
		},
		{
			name:  "MongoDB $gt operator",
			input: "{age: {$gt: 0}}",
		},
		{
			name:  "MongoDB $regex injection",
			input: "{username: {$regex: '.*'}}",
		},
		{
			name:  "JavaScript injection",
			input: "'; return true; var x='",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patterns := DetectSuspiciousPatterns(tt.input)
			
			hasSuspicious := false
			for _, pattern := range patterns {
				if strings.Contains(pattern, "NOSQL_INJECTION") || strings.Contains(pattern, "SQL_INJECTION") {
					hasSuspicious = true
					break
				}
			}

			if !hasSuspicious {
				t.Errorf("NoSQL/SQL injection not detected in: %s", tt.input)
			}
		})
	}
}

// TestMaliciousControlCharacters tests control character detection
func TestMaliciousControlCharacters(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "null byte",
			input: "admin\x00",
		},
		{
			name:  "bell character",
			input: "test\x07",
		},
		{
			name:  "escape character",
			input: "test\x1b",
		},
		{
			name:  "vertical tab",
			input: "test\x0b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patterns := DetectSuspiciousPatterns(tt.input)
			
			hasControlChar := false
			for _, pattern := range patterns {
				if strings.Contains(pattern, "CONTROL_CHARACTERS") || strings.Contains(pattern, "NULL_BYTE") {
					hasControlChar = true
					break
				}
			}

			if !hasControlChar {
				t.Errorf("Control characters not detected in: %v", []byte(tt.input))
			}
		})
	}
}

// TestMalformedInputs tests various malformed inputs
func TestMalformedInputs(t *testing.T) {
	validator := NewDefaultInputValidator()

	tests := []struct {
		name      string
		request   *OrderRequest
		wantError bool
	}{
		{
			name: "extremely long client_id",
			request: &OrderRequest{
				ClientID: strings.Repeat("a", 10000),
				Symbol:   "BTCUSD",
				Side:     "buy",
				Price:    50000.0,
				Quantity: 1.0,
			},
			wantError: true,
		},
		{
			name: "client_id with unicode exploits",
			request: &OrderRequest{
				ClientID: "admin\u202e",
				Symbol:   "BTCUSD",
				Side:     "buy",
				Price:    50000.0,
				Quantity: 1.0,
			},
			wantError: true,
		},
		{
			name: "negative zero price",
			request: &OrderRequest{
				ClientID: "client123",
				Symbol:   "BTCUSD",
				Side:     "buy",
				Price:    -0.0,
				Quantity: 1.0,
			},
			wantError: true,
		},
		{
			name: "denormalized number - within precision",
			request: &OrderRequest{
				ClientID: "client123",
				Symbol:   "BTCUSD",
				Side:     "buy",
				Price:    50000.00000001, // 8 decimals, should pass
				Quantity: 1.0,
			},
			wantError: false,
		},
		{
			name: "symbol with zero-width characters",
			request: &OrderRequest{
				ClientID: "client123",
				Symbol:   "BTC\u200bUSD",
				Side:     "buy",
				Price:    50000.0,
				Quantity: 1.0,
			},
			wantError: true,
		},
		{
			name: "mixed case side with whitespace",
			request: &OrderRequest{
				ClientID: "client123",
				Symbol:   "BTCUSD",
				Side:     " BuY ",
				Price:    50000.0,
				Quantity: 1.0,
			},
			wantError: false, // Should be normalized
		},
		{
			name: "price with excessive precision",
			request: &OrderRequest{
				ClientID: "client123",
				Symbol:   "BTCUSD",
				Side:     "buy",
				Price:    50000.123456789123456,
				Quantity: 1.0,
			},
			wantError: true,
		},
		{
			name: "quantity with scientific notation",
			request: &OrderRequest{
				ClientID: "client123",
				Symbol:   "BTCUSD",
				Side:     "buy",
				Price:    50000.0,
				Quantity: 1e10,
			},
			wantError: true, // Exceeds max quantity
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateOrderRequest(tt.request)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateOrderRequest() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

// TestSuspiciousActivityLogger tests the activity logging system
func TestSuspiciousActivityLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	config := DefaultSecurityConfig()
	config.BlockAfterAttempts = 3

	sal := NewSuspiciousActivityLogger(logger, config)

	// Test logging multiple attempts
	source := "192.168.1.100"
	
	// First attempt
	sal.LogSuspiciousInput(source, "CLIENT_ID", "SQL_INJECTION", "admin' OR '1'='1", "low")
	if sal.ShouldBlock(source) {
		t.Error("Should not block after first attempt")
	}

	// Second attempt
	sal.LogSuspiciousInput(source, "CLIENT_ID", "XSS", "<script>alert(1)</script>", "medium")
	if sal.ShouldBlock(source) {
		t.Error("Should not block after second attempt")
	}

	// Third attempt
	sal.LogSuspiciousInput(source, "CLIENT_ID", "CMD_INJECTION", "; rm -rf /", "high")
	if !sal.ShouldBlock(source) {
		t.Error("Should block after third attempt")
	}

	// Check activity stats
	stats := sal.GetActivityStats(source)
	if stats == nil {
		t.Fatal("Activity stats should exist")
	}

	if stats.Count != 3 {
		t.Errorf("Expected 3 attempts, got %d", stats.Count)
	}

	if stats.SuspicionLevel != "critical" {
		t.Errorf("Expected critical level, got %s", stats.SuspicionLevel)
	}

	// Check logs were written
	logOutput := buf.String()
	if !strings.Contains(logOutput, "SECURITY") {
		t.Error("Log should contain SECURITY marker")
	}

	if !strings.Contains(logOutput, source) {
		t.Error("Log should contain source IP")
	}
}

// TestEnhancedValidatorWithSecurity tests enhanced validation
func TestEnhancedValidatorWithSecurity(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	
	validator := NewEnhancedInputValidator(
		DefaultValidationConfig(),
		DefaultSecurityConfig(),
		logger,
	)

	tests := []struct {
		name      string
		request   *OrderRequest
		source    string
		wantError bool
	}{
		{
			name: "clean request",
			request: &OrderRequest{
				ClientID: "client123",
				Symbol:   "BTCUSD",
				Side:     "buy",
				Price:    50000.0,
				Quantity: 1.0,
			},
			source:    "192.168.1.1",
			wantError: false,
		},
		{
			name: "SQL injection in client_id",
			request: &OrderRequest{
				ClientID: "admin' OR '1'='1",
				Symbol:   "BTCUSD",
				Side:     "buy",
				Price:    50000.0,
				Quantity: 1.0,
			},
			source:    "192.168.1.100",
			wantError: true,
		},
		{
			name: "XSS in symbol",
			request: &OrderRequest{
				ClientID: "client123",
				Symbol:   "<script>alert(1)</script>",
				Side:     "buy",
				Price:    50000.0,
				Quantity: 1.0,
			},
			source:    "192.168.1.101",
			wantError: true,
		},
		{
			name: "command injection in side",
			request: &OrderRequest{
				ClientID: "client123",
				Symbol:   "BTCUSD",
				Side:     "buy; rm -rf /",
				Price:    50000.0,
				Quantity: 1.0,
			},
			source:    "192.168.1.102",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateOrderRequestWithSecurity(tt.request, tt.source)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateOrderRequestWithSecurity() error = %v, wantError %v", err, tt.wantError)
			}

			// Check if suspicious activity was logged for malicious inputs
			if tt.wantError {
				logOutput := buf.String()
				if !strings.Contains(logOutput, "SECURITY") {
					t.Error("Suspicious activity should be logged")
				}
			}
		})
	}
}

// TestSecurityReport tests security report generation
func TestSecurityReport(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	
	validator := NewEnhancedInputValidator(
		DefaultValidationConfig(),
		DefaultSecurityConfig(),
		logger,
	)

	// Generate some suspicious activity
	sources := []string{"192.168.1.1", "192.168.1.2", "192.168.1.3"}
	for _, source := range sources {
		validator.ValidateClientIDWithSecurity("admin' OR '1'='1", source)
		validator.ValidateClientIDWithSecurity("<script>alert(1)</script>", source)
	}

	// Generate report
	report := validator.GenerateSecurityReport()

	if report.TotalSources != len(sources) {
		t.Errorf("Expected %d sources, got %d", len(sources), report.TotalSources)
	}

	if len(report.TopPatterns) == 0 {
		t.Error("Report should contain detected patterns")
	}

	// Export as JSON
	jsonReport, err := validator.ExportSecurityLog()
	if err != nil {
		t.Fatalf("Failed to export security log: %v", err)
	}

	if !strings.Contains(jsonReport, "timestamp") {
		t.Error("JSON report should contain timestamp")
	}
}

// TestBlockingAfterSuspiciousActivity tests automatic blocking
func TestBlockingAfterSuspiciousActivity(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	config := DefaultSecurityConfig()
	config.BlockAfterAttempts = 2

	validator := NewEnhancedInputValidator(
		DefaultValidationConfig(),
		config,
		logger,
	)

	source := "192.168.1.100"

	// First malicious attempt
	err := validator.ValidateClientIDWithSecurity("'; DROP TABLE--", source)
	if err == nil {
		t.Error("Should detect malicious input")
	}

	// Second malicious attempt
	err = validator.ValidateClientIDWithSecurity("<script>alert(1)</script>", source)
	if err == nil {
		t.Error("Should detect malicious input")
	}

	// Third attempt should be blocked
	err = validator.ValidateClientIDWithSecurity("client123", source)
	if err == nil {
		t.Error("Should be blocked after repeated suspicious activity")
	} else {
		if !strings.Contains(err.Error(), "blocked") {
			t.Errorf("Error should mention blocking, got: %v", err)
		}
	}
}

// TestRateLimitReset tests stat reset functionality
func TestRateLimitReset(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	
	sal := NewSuspiciousActivityLogger(logger, DefaultSecurityConfig())
	
	source := "192.168.1.100"
	
	// Log some activity
	sal.LogSuspiciousInput(source, "TEST", "PATTERN", "payload", "low")
	
	stats := sal.GetActivityStats(source)
	if stats == nil || stats.Count != 1 {
		t.Fatal("Stats should be recorded")
	}

	// Reset stats
	sal.ResetStats(source)
	
	stats = sal.GetActivityStats(source)
	if stats != nil {
		t.Error("Stats should be cleared after reset")
	}
}

// Benchmark malicious input detection
func BenchmarkDetectSuspiciousPatterns(b *testing.B) {
	input := "admin' OR '1'='1; DROP TABLE users; <script>alert(1)</script>"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DetectSuspiciousPatterns(input)
	}
}

func BenchmarkEnhancedValidation(b *testing.B) {
	validator := NewEnhancedInputValidator(
		DefaultValidationConfig(),
		DefaultSecurityConfig(),
		log.Default(),
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		validator.ValidateClientIDWithSecurity("client123", "192.168.1.1")
	}
}
