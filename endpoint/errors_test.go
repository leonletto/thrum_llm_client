package endpoint

import (
	"errors"
	"net/http"
	"testing"
)

func TestSentinels_AreDistinct(t *testing.T) {
	sents := []error{
		ErrAuthenticationRequired, ErrRateLimited,
		ErrBadRequest, ErrForbidden, ErrNotFound,
		ErrTimeout, ErrServiceUnavailable, ErrServerError,
	}
	for i, a := range sents {
		for j, b := range sents {
			if i != j && a == b {
				t.Fatalf("sentinels at %d and %d are the same value: %v", i, j, a)
			}
		}
	}
}

func TestHTTPStatusToSentinel(t *testing.T) {
	cases := []struct {
		code int
		want error
	}{
		{http.StatusBadRequest, ErrBadRequest},                 // 400
		{http.StatusUnauthorized, ErrAuthenticationRequired},   // 401
		{http.StatusForbidden, ErrForbidden},                   // 403
		{http.StatusNotFound, ErrNotFound},                     // 404
		{http.StatusRequestTimeout, ErrTimeout},                // 408
		{http.StatusUnprocessableEntity, ErrBadRequest},        // 422
		{http.StatusTooManyRequests, ErrRateLimited},           // 429
		{http.StatusInternalServerError, ErrServerError},       // 500
		{http.StatusBadGateway, ErrServerError},                // 502
		{http.StatusServiceUnavailable, ErrServiceUnavailable}, // 503
		{http.StatusGatewayTimeout, ErrTimeout},                // 504
		{http.StatusHTTPVersionNotSupported, ErrServerError},   // 505
		{http.StatusOK, nil},                                   // 200
		{http.StatusNoContent, nil},                            // 204
		{499, nil},                                             // unmapped 4xx
	}
	for _, tc := range cases {
		got := httpStatusToSentinel(tc.code)
		if tc.want == nil && got != nil {
			t.Errorf("code=%d: got %v; want nil", tc.code, got)
			continue
		}
		if tc.want != nil && !errors.Is(got, tc.want) {
			t.Errorf("code=%d: got %v; want errors.Is == %v", tc.code, got, tc.want)
		}
	}
}

func TestEndpointError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *EndpointError
		expected string
	}{
		{
			name: "with provider",
			err: &EndpointError{
				Endpoint: "http://localhost:11434",
				Provider: "ollama",
				Op:       "HealthCheck",
				Err:      errors.New("connection refused"),
			},
			expected: "ollama: HealthCheck on http://localhost:11434: connection refused",
		},
		{
			name: "without provider",
			err: &EndpointError{
				Endpoint: "http://localhost:11434",
				Op:       "HealthCheck",
				Err:      errors.New("connection refused"),
			},
			expected: "HealthCheck on http://localhost:11434: connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.expected {
				t.Errorf("Error() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestEndpointError_Unwrap(t *testing.T) {
	innerErr := errors.New("connection refused")
	err := &EndpointError{
		Endpoint: "http://localhost:11434",
		Provider: "ollama",
		Op:       "HealthCheck",
		Err:      innerErr,
	}

	if unwrapped := err.Unwrap(); unwrapped != innerErr {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, innerErr)
	}

	// Test errors.Is
	if !errors.Is(err, innerErr) {
		t.Error("errors.Is should return true for wrapped error")
	}
}

func TestNewEndpointError(t *testing.T) {
	innerErr := errors.New("timeout")
	err := NewEndpointError("http://localhost:11434", "ollama", "ListModels", innerErr)

	if err.Endpoint != "http://localhost:11434" {
		t.Errorf("Endpoint = %q, want %q", err.Endpoint, "http://localhost:11434")
	}
	if err.Provider != "ollama" {
		t.Errorf("Provider = %q, want %q", err.Provider, "ollama")
	}
	if err.Op != "ListModels" {
		t.Errorf("Op = %q, want %q", err.Op, "ListModels")
	}
	if err.Err != innerErr {
		t.Errorf("Err = %v, want %v", err.Err, innerErr)
	}
}

func TestErrorConstants(t *testing.T) {
	// Test that error constants are defined
	errorConstants := []error{
		ErrNoEndpointsAvailable,
		ErrProviderNotFound,
		ErrHealthCheckFailed,
		ErrModelNotFound,
		ErrAuthenticationRequired,
		ErrInvalidEndpoint,
		ErrRateLimited,
		ErrProviderNotConfigured,
		ErrInvalidConfiguration,
		ErrBadRequest,
		ErrForbidden,
		ErrNotFound,
		ErrTimeout,
		ErrServiceUnavailable,
		ErrServerError,
	}

	for _, err := range errorConstants {
		if err == nil {
			t.Error("Error constant should not be nil")
		}
		if err.Error() == "" {
			t.Error("Error constant should have non-empty message")
		}
	}
}

func TestErrorConstants_Uniqueness(t *testing.T) {
	// Test that error messages are unique
	errors := map[string]error{
		"no healthy endpoints available": ErrNoEndpointsAvailable,
		"provider not found":             ErrProviderNotFound,
		"health check failed":            ErrHealthCheckFailed,
		"model not found":                ErrModelNotFound,
		"authentication required":        ErrAuthenticationRequired,
		"invalid endpoint URL":           ErrInvalidEndpoint,
		"rate limited":                   ErrRateLimited,
		"provider not configured":        ErrProviderNotConfigured,
		"invalid configuration":          ErrInvalidConfiguration,
		"bad request":                    ErrBadRequest,
		"forbidden":                      ErrForbidden,
		"not found":                      ErrNotFound,
		"timeout":                        ErrTimeout,
		"service unavailable":            ErrServiceUnavailable,
		"server error":                   ErrServerError,
	}

	for msg, err := range errors {
		if err.Error() != msg {
			t.Errorf("Error message mismatch: got %q, want %q", err.Error(), msg)
		}
	}
}

func TestErrorConstants_ErrorsIs(t *testing.T) {
	// Test that errors.Is works with our error constants
	wrappedErr := NewEndpointError("http://localhost:11434", "ollama", "HealthCheck", ErrHealthCheckFailed)

	if !errors.Is(wrappedErr, ErrHealthCheckFailed) {
		t.Error("errors.Is should return true for wrapped ErrHealthCheckFailed")
	}

	if errors.Is(wrappedErr, ErrModelNotFound) {
		t.Error("errors.Is should return false for different error type")
	}
}

func TestErrCapabilityNotSupported_IsDistinct(t *testing.T) {
	if ErrCapabilityNotSupported == nil {
		t.Fatal("ErrCapabilityNotSupported is nil")
	}
	if ErrCapabilityNotSupported == ErrProviderNotSupported {
		t.Fatal("ErrCapabilityNotSupported must be distinct from ErrProviderNotSupported")
	}
}

func TestErrPollTimeout_IsDistinct(t *testing.T) {
	if ErrPollTimeout == ErrTimeout {
		t.Fatal("ErrPollTimeout must be distinct from ErrTimeout")
	}
}
