package errors

import (
	"fmt"
	"net/http"
)

// Category groups error codes by functional area.
type Category string

const (
	CatAuthentication Category = "authentication"
	CatAuthorization  Category = "authorization"
	CatBalance        Category = "balance"
	CatQuota          Category = "quota"
	CatProvider       Category = "provider"
	CatPayment        Category = "payment"
	CatValidation     Category = "validation"
	CatInternal       Category = "internal"
	CatNotFound       Category = "not_found"
	CatNotImplemented Category = "not_implemented"
	CatTimeout        Category = "timeout"
)

// Code is a unique error code within a category.
type Code string

// ErrorCode defines a single error code with its HTTP status and description.
type ErrorCode struct {
	Code       Code
	Category   Category
	HTTPStatus int
	Message    string
	Description string // detailed description for API consumers
}

// Registry holds all defined error codes.
type Registry struct {
	codes map[Code]ErrorCode
}

// Global registry instance.
var reg = &Registry{codes: make(map[Code]ErrorCode)}

// Define registers a new error code. Panics if code is already registered.
func Define(code Code, cat Category, httpStatus int, message, description string) ErrorCode {
	ec := ErrorCode{
		Code:        code,
		Category:    cat,
		HTTPStatus:  httpStatus,
		Message:     message,
		Description: description,
	}
	if _, exists := reg.codes[code]; exists {
		panic("error code already defined: " + string(code))
	}
	reg.codes[code] = ec
	return ec
}

// Get returns the ErrorCode for a code, or false if not found.
func Get(code Code) (ErrorCode, bool) {
	ec, ok := reg.codes[code]
	return ec, ok
}

// All returns all registered error codes.
func All() []ErrorCode {
	out := make([]ErrorCode, 0, len(reg.codes))
	for _, ec := range reg.codes {
		out = append(out, ec)
	}
	return out
}

// APIError is a structured error returned by API handlers.
type APIError struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	Type    string `json:"type"`
}

// Error implements error.
func (e *APIError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// ToResponse returns the Gin JSON representation of the error.
func (e *APIError) ToResponse() map[string]interface{} {
	return map[string]interface{}{
		"error": map[string]interface{}{
			"code":    e.Code,
			"message": e.Message,
			"type":    e.Type,
		},
	}
}

// NewAPIError creates an APIError for a given error code.
func NewAPIError(code Code) *APIError {
	ec, ok := Get(code)
	if !ok {
		return &APIError{Code: code, Message: string(code), Type: "internal_error"}
	}
	return &APIError{
		Code:    code,
		Message: ec.Message,
		Type:    string(ec.Category),
	}
}

// WithMessage returns a copy of the APIError with a custom message.
func (e *APIError) WithMessage(msg string) *APIError {
	return &APIError{Code: e.Code, Message: msg, Type: e.Type}
}

// Register all standard HiveMachine error codes.
func init() {
	// Authentication (401)
	Define("AUTH_MISSING", CatAuthentication, http.StatusUnauthorized,
		"missing authentication credentials",
		"Request must include an Authorization header with a valid API key.")
	Define("AUTH_INVALID", CatAuthentication, http.StatusUnauthorized,
		"invalid or malformed API key",
		"The provided API key is not recognized or is malformed.")
	Define("AUTH_EXPIRED", CatAuthentication, http.StatusUnauthorized,
		"API key has expired",
		"The API key has expired. Generate a new key from the dashboard.")

	// Authorization (403)
	Define("FORBIDDEN", CatAuthorization, http.StatusForbidden,
		"operation not permitted",
		"The authenticated API key does not have permission to perform this operation.")
	Define("TIER_INSUFFICIENT", CatAuthorization, http.StatusForbidden,
		"minimum trust tier not met",
		"This operation requires a higher trust tier. Current tier is insufficient for the requested model or provider.")

	// Balance (402 / 409)
	Define("BALANCE_INSUFFICIENT", CatBalance, http.StatusPaymentRequired,
		"insufficient balance",
		"The account balance is insufficient for this operation. Please add funds via /v1/balance/checkout.")
	Define("BALANCE_RAIL_MISMATCH", CatBalance, http.StatusConflict,
		"balance rail mismatch",
		"The requested operation is on a different payment rail than the current balance. Use the correct rail or transfer via external exchange.")
	Define("BALANCE_NOT_FOUND", CatBalance, http.StatusNotFound,
		"balance account not found",
		"No balance account exists for this API key. Create an account first.")

	// Quota (429)
	Define("QUOTA_EXCEEDED", CatQuota, http.StatusTooManyRequests,
		"quota limit exceeded",
		"The token quota for this billing period has been exhausted. Wait for the period to reset or request a quota increase.")
	Define("RATE_LIMITED", CatQuota, http.StatusTooManyRequests,
		"rate limit exceeded",
		"Too many requests. Slow down and retry after the indicated delay.")

	// Provider (503 / 502)
	Define("PROVIDER_UNAVAILABLE", CatProvider, http.StatusServiceUnavailable,
		"no healthy providers available",
		"All registered providers are currently unavailable or unhealthy. Try again later.")
	Define("PROVIDER_CAPACITY_EXCEEDED", CatProvider, http.StatusServiceUnavailable,
		"provider at maximum capacity",
		"The provider is handling the maximum number of concurrent requests. Retry after a short delay.")
	Define("PROVIDER_MIN_ASK", CatProvider, http.StatusBadRequest,
		"offered price below provider minimum",
		"The calculated price is below the provider's minimum acceptable price. Increase the bid or select a different provider.")
	Define("PROVIDER_NOT_FOUND", CatProvider, http.StatusNotFound,
		"provider not found",
		"The specified provider ID does not exist in the registry.")

	// Payment (402)
	Define("PAYMENT_FAILED", CatPayment, http.StatusPaymentRequired,
		"payment processing failed",
		"The payment could not be processed. Check payment details and try again.")
	Define("PAYMENT_RAIL_UNSUPPORTED", CatPayment, http.StatusBadRequest,
		"payment rail not supported",
		"The specified payment rail (stripe/tap/tnk) is not supported or not configured.")
	Define("PAYMENT_WEBHOOK_INVALID", CatPayment, http.StatusBadRequest,
		"webhook signature verification failed",
		"The webhook payload signature does not match the expected signature. Ensure the webhook secret is correctly configured.")

	// Validation (400)
	Define("VALIDATION_INVALID_REQUEST", CatValidation, http.StatusBadRequest,
		"invalid request parameters",
		"One or more request parameters are missing, malformed, or invalid. Check the API documentation for the correct format.")
	Define("VALIDATION_MODEL_UNSUPPORTED", CatValidation, http.StatusBadRequest,
		"model not supported",
		"The requested model is not available or not supported by any registered provider.")
	Define("VALIDATION_MESSAGE_EMPTY", CatValidation, http.StatusBadRequest,
		"messages array is empty",
		"The messages array must contain at least one message.")
	Define("VALIDATION_CONTENT_MISSING", CatValidation, http.StatusBadRequest,
		"message content is missing",
		"Each message must have a non-empty content field.")

	// Internal (500)
	Define("INTERNAL_ERROR", CatInternal, http.StatusInternalServerError,
		"internal server error",
		"An unexpected error occurred. Please try again later. If the problem persists, contact support.")
	Define("INTERNAL_TIMEOUT", CatInternal, http.StatusGatewayTimeout,
		"upstream request timed out",
		"The Rust core did not respond within the expected time. Try a smaller request or try again later.")
	Define("INTERNAL_GRPC", CatInternal, http.StatusBadGateway,
		"gRPC communication error",
		"Failed to communicate with the Rust core. Ensure the core service is running.")

	// Not Found (404)
	Define("NOT_FOUND", CatNotFound, http.StatusNotFound,
		"resource not found",
		"The requested resource does not exist or has been removed.")
	Define("MODEL_NOT_FOUND", CatNotFound, http.StatusNotFound,
		"model not found",
		"The specified model is not registered in the model registry.")

	// Not Implemented (501)
	Define("NOT_IMPLEMENTED", CatNotImplemented, http.StatusNotImplemented,
		"operation not implemented",
		"This feature is not yet supported by the Rust core. See documentation for available endpoints.")

	// Timeout (408 / 504)
	Define("REQUEST_TIMEOUT", CatTimeout, http.StatusRequestTimeout,
		"request timeout",
		"The request took too long to process. Try a smaller request or reduce concurrency.")
}

// Sentinel errors for use across packages.
var (
	ErrMissingAuth     = NewAPIError("AUTH_MISSING")
	ErrInvalidAuth     = NewAPIError("AUTH_INVALID")
	ErrExpiredAuth     = NewAPIError("AUTH_EXPIRED")
	ErrForbidden       = NewAPIError("FORBIDDEN")
	ErrTierInsufficient = NewAPIError("TIER_INSUFFICIENT")

	ErrInsufficientBalance = NewAPIError("BALANCE_INSUFFICIENT")
	ErrRailMismatch        = NewAPIError("BALANCE_RAIL_MISMATCH")
	ErrBalanceNotFound     = NewAPIError("BALANCE_NOT_FOUND")

	ErrQuotaExceeded = NewAPIError("QUOTA_EXCEEDED")
	ErrRateLimited   = NewAPIError("RATE_LIMITED")

	ErrProviderUnavailable    = NewAPIError("PROVIDER_UNAVAILABLE")
	ErrProviderCapacity       = NewAPIError("PROVIDER_CAPACITY_EXCEEDED")
	ErrProviderMinAsk         = NewAPIError("PROVIDER_MIN_ASK")
	ErrProviderNotFound       = NewAPIError("PROVIDER_NOT_FOUND")

	ErrPaymentFailed       = NewAPIError("PAYMENT_FAILED")
	ErrPaymentRailUnsupport = NewAPIError("PAYMENT_RAIL_UNSUPPORTED")
	ErrWebhookInvalid       = NewAPIError("PAYMENT_WEBHOOK_INVALID")

	ErrValidation       = NewAPIError("VALIDATION_INVALID_REQUEST")
	ErrModelUnsupported = NewAPIError("VALIDATION_MODEL_UNSUPPORTED")
	ErrMessageEmpty     = NewAPIError("VALIDATION_MESSAGE_EMPTY")
	ErrContentMissing   = NewAPIError("VALIDATION_CONTENT_MISSING")

	ErrInternal   = NewAPIError("INTERNAL_ERROR")
	ErrTimeout    = NewAPIError("INTERNAL_TIMEOUT")
	ErrGRPC       = NewAPIError("INTERNAL_GRPC")

	ErrNotFound       = NewAPIError("NOT_FOUND")
	ErrModelNotFound  = NewAPIError("MODEL_NOT_FOUND")
	ErrNotImplemented = NewAPIError("NOT_IMPLEMENTED")
)
