package errors

import (
	"net/http"
	"testing"
)

func TestDefine(t *testing.T) {
	// Define should panic if code already exists.
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate define")
		}
	}()
	Define("TEST_DUP", CatInternal, 500, "dup", "duplicate")
	Define("TEST_DUP", CatInternal, 500, "dup", "duplicate")
}

func TestGet(t *testing.T) {
	ec, ok := Get("AUTH_MISSING")
	if !ok {
		t.Fatal("AUTH_MISSING not found")
	}
	if ec.HTTPStatus != http.StatusUnauthorized {
		t.Errorf("AUTH_MISSING status = %d, want %d", ec.HTTPStatus, http.StatusUnauthorized)
	}
	if ec.Category != CatAuthentication {
		t.Errorf("AUTH_MISSING category = %v, want %v", ec.Category, CatAuthentication)
	}

	_, ok = Get("NONEXISTENT")
	if ok {
		t.Error("expected false for nonexistent code")
	}
}

func TestAll(t *testing.T) {
	codes := All()
	if len(codes) == 0 {
		t.Fatal("All() returned empty")
	}
	// Should contain our standard codes.
	found := false
	for _, ec := range codes {
		if ec.Code == "AUTH_MISSING" {
			found = true
			break
		}
	}
	if !found {
		t.Error("AUTH_MISSING not in All()")
	}
}

func TestAPIError(t *testing.T) {
	apiErr := NewAPIError("AUTH_MISSING")
	if apiErr.Code != "AUTH_MISSING" {
		t.Errorf("Code = %v, want AUTH_MISSING", apiErr.Code)
	}
	if apiErr.Type != "authentication" {
		t.Errorf("Type = %v, want authentication", apiErr.Type)
	}
	if apiErr.Message != "missing authentication credentials" {
		t.Errorf("Message = %v, want 'missing authentication credentials'", apiErr.Message)
	}
}

func TestAPIErrorWithMessage(t *testing.T) {
	apiErr := NewAPIError("AUTH_MISSING").WithMessage("custom message")
	if apiErr.Message != "custom message" {
		t.Errorf("Message = %v, want 'custom message'", apiErr.Message)
	}
	if apiErr.Code != "AUTH_MISSING" {
		t.Errorf("Code = %v, want AUTH_MISSING", apiErr.Code)
	}
}

func TestAPIErrorError(t *testing.T) {
	apiErr := NewAPIError("AUTH_MISSING")
	expected := "AUTH_MISSING: missing authentication credentials"
	if got := apiErr.Error(); got != expected {
		t.Errorf("Error() = %v, want %v", got, expected)
	}
}

func TestAPIErrorToResponse(t *testing.T) {
	apiErr := NewAPIError("AUTH_MISSING")
	resp := apiErr.ToResponse()
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatal("ToResponse() missing error key")
	}
	if errObj["code"].(Code) != "AUTH_MISSING" {
		t.Errorf("code = %v, want AUTH_MISSING", errObj["code"])
	}
	if errObj["type"].(string) != "authentication" {
		t.Errorf("type = %v, want authentication", errObj["type"])
	}
}

func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		err     *APIError
		code    Code
		httpStatus int
		cat     Category
	}{
		{ErrMissingAuth, "AUTH_MISSING", http.StatusUnauthorized, CatAuthentication},
		{ErrInvalidAuth, "AUTH_INVALID", http.StatusUnauthorized, CatAuthentication},
		{ErrForbidden, "FORBIDDEN", http.StatusForbidden, CatAuthorization},
		{ErrInsufficientBalance, "BALANCE_INSUFFICIENT", http.StatusPaymentRequired, CatBalance},
		{ErrQuotaExceeded, "QUOTA_EXCEEDED", http.StatusTooManyRequests, CatQuota},
		{ErrProviderUnavailable, "PROVIDER_UNAVAILABLE", http.StatusServiceUnavailable, CatProvider},
		{ErrValidation, "VALIDATION_INVALID_REQUEST", http.StatusBadRequest, CatValidation},
		{ErrInternal, "INTERNAL_ERROR", http.StatusInternalServerError, CatInternal},
		{ErrNotFound, "NOT_FOUND", http.StatusNotFound, CatNotFound},
		{ErrNotImplemented, "NOT_IMPLEMENTED", http.StatusNotImplemented, CatNotImplemented},
		{ErrRateLimited, "RATE_LIMITED", http.StatusTooManyRequests, CatQuota},
	}
	for _, tt := range tests {
		if tt.err.Code != tt.code {
			t.Errorf("%v.Code = %v, want %v", tt.err, tt.err.Code, tt.code)
		}
		ec, ok := Get(tt.code)
		if !ok {
			t.Errorf("code %v not registered", tt.code)
			continue
		}
		if ec.HTTPStatus != tt.httpStatus {
			t.Errorf("%v HTTPStatus = %d, want %d", tt.err, ec.HTTPStatus, tt.httpStatus)
		}
		if ec.Category != tt.cat {
			t.Errorf("%v Category = %v, want %v", tt.err, ec.Category, tt.cat)
		}
	}
}
