package endpoint

import "errors"

// Error types for endpoint operations.
var (
	// ErrNoEndpointsAvailable is returned when no healthy endpoints are available.
	ErrNoEndpointsAvailable = errors.New("no healthy endpoints available")

	// ErrProviderNotFound is returned when a requested provider is not registered.
	ErrProviderNotFound = errors.New("provider not found")

	// ErrHealthCheckFailed is returned when a health check fails.
	ErrHealthCheckFailed = errors.New("health check failed")

	// ErrModelNotFound is returned when a requested model is not available.
	ErrModelNotFound = errors.New("model not found")

	// ErrAuthenticationRequired is returned when authentication is required but not provided.
	ErrAuthenticationRequired = errors.New("authentication required")

	// ErrInvalidEndpoint is returned when an endpoint URL is invalid.
	ErrInvalidEndpoint = errors.New("invalid endpoint URL")

	// ErrRateLimited is returned when the endpoint rate limit is exceeded.
	ErrRateLimited = errors.New("rate limited")

	// ErrProviderNotConfigured is returned when a provider is not configured.
	ErrProviderNotConfigured = errors.New("provider not configured")

	// ErrInvalidConfiguration is returned when configuration is invalid.
	ErrInvalidConfiguration = errors.New("invalid configuration")

	// ErrZaiEmptyToolCall is returned by the zai adapter when a tool-call
	// response is received whose arguments cannot be parsed as JSON
	// (typically an empty string from a known sporadic provider failure
	// mode). The DefaultRetryPolicy's zai_empty_tool_call predicate
	// matches this sentinel via errors.Is and retries the request.
	ErrZaiEmptyToolCall = errors.New("zai: empty or malformed tool-call arguments")

	// ErrBadRequest is returned for HTTP 400 (Bad Request) and 422
	// (Unprocessable Entity) — the server rejected the request as
	// malformed or semantically invalid.
	ErrBadRequest = errors.New("bad request")

	// ErrForbidden is returned for HTTP 403 — authentication succeeded
	// but the credential lacks permission for this operation.
	ErrForbidden = errors.New("forbidden")

	// ErrNotFound is returned for HTTP 404 — the requested resource
	// (model, endpoint, etc.) does not exist on the server.
	ErrNotFound = errors.New("not found")

	// ErrTimeout is returned for HTTP 408 (Request Timeout) and 504
	// (Gateway Timeout) — distinct from a transport-level deadline,
	// this signals the upstream timed out processing the request.
	ErrTimeout = errors.New("timeout")

	// ErrServiceUnavailable is returned for HTTP 503 — the provider
	// is temporarily unable to handle the request (overload,
	// maintenance). Callers may retry with backoff.
	ErrServiceUnavailable = errors.New("service unavailable")

	// ErrServerError is returned for HTTP 5xx codes not otherwise
	// mapped (500, 502, 505, …). Catch-all for upstream failures.
	ErrServerError = errors.New("server error")

	// ErrCapabilityNotSupported is returned when a provider does not
	// support the requested capability. Distinct from
	// ErrProviderNotSupported (model-registry "this canonical model
	// is not mapped for this provider"); this sentinel signals that
	// the provider as a whole has no adapter for the requested
	// modality (e.g. anthropic and ollama have no image client).
	ErrCapabilityNotSupported = errors.New("capability not supported by provider")

	// ErrPollTimeout is returned by pollAsync (and therefore
	// VideoClient.WaitVideo) when the client-side polling loop
	// exceeds PollOptions.MaxWait without observing a terminal job
	// status. Distinct from ErrTimeout (HTTP 408/504 — upstream
	// took too long processing a single request). Callers that wait
	// long-running video jobs and want to disambiguate should
	// errors.Is against ErrPollTimeout specifically.
	ErrPollTimeout = errors.New("poll timeout")
)

// httpStatusToSentinel maps an HTTP status code to the canonical
// typed sentinel for that class of failure. Returns nil for 2xx
// statuses and for any code that does not correspond to one of the
// defined sentinels — callers should treat a nil return as "fall
// through to a generic error". Adapters call this helper from their
// status-check failure paths so all four providers expose a uniform
// typed-error surface to callers using errors.Is.
func httpStatusToSentinel(code int) error {
	if code >= 200 && code < 300 {
		return nil
	}
	switch code {
	case 400, 422:
		return ErrBadRequest
	case 401:
		return ErrAuthenticationRequired
	case 403:
		return ErrForbidden
	case 404:
		return ErrNotFound
	case 408, 504:
		return ErrTimeout
	case 429:
		return ErrRateLimited
	case 503:
		return ErrServiceUnavailable
	}
	if code >= 500 && code < 600 {
		return ErrServerError
	}
	return nil
}

// EndpointError wraps an error with additional context about the endpoint.
type EndpointError struct {
	Endpoint string
	Provider string
	Op       string // operation that failed
	Err      error
}

// Error implements the error interface.
func (e *EndpointError) Error() string {
	if e.Provider != "" {
		return e.Provider + ": " + e.Op + " on " + e.Endpoint + ": " + e.Err.Error()
	}
	return e.Op + " on " + e.Endpoint + ": " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *EndpointError) Unwrap() error {
	return e.Err
}

// NewEndpointError creates a new EndpointError.
func NewEndpointError(endpoint, provider, op string, err error) *EndpointError {
	return &EndpointError{
		Endpoint: endpoint,
		Provider: provider,
		Op:       op,
		Err:      err,
	}
}
