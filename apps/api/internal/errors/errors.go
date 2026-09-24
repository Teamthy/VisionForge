// Package errors defines typed application errors with HTTP status mapping.
package errors

import (
	"errors"
	"fmt"
	"net/http"
)

// Kind classifies the error category for HTTP mapping.
type Kind string

const (
	KindValidation   Kind = "VALIDATION_ERROR"
	KindUnauthorized Kind = "UNAUTHORIZED"
	KindForbidden    Kind = "FORBIDDEN"
	KindNotFound     Kind = "NOT_FOUND"
	KindConflict     Kind = "CONFLICT"
	KindRateLimited  Kind = "RATE_LIMITED"
	KindStorage      Kind = "STORAGE_ERROR"
	KindDatabase     Kind = "DATABASE_ERROR"
	KindModel        Kind = "MODEL_ERROR"
	KindInference    Kind = "INFERENCE_ERROR"
	KindQueue        Kind = "QUEUE_ERROR"
	KindInternal     Kind = "INTERNAL_ERROR"
)

// Status maps a Kind to an HTTP status code.
func (k Kind) Status() int {
	switch k {
	case KindValidation:
		return http.StatusBadRequest
	case KindUnauthorized:
		return http.StatusUnauthorized
	case KindForbidden:
		return http.StatusForbidden
	case KindNotFound:
		return http.StatusNotFound
	case KindConflict:
		return http.StatusConflict
	case KindRateLimited:
		return http.StatusTooManyRequests
	case KindStorage, KindDatabase, KindQueue, KindModel, KindInference, KindInternal:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

// AppError is a typed application error carrying a Kind, message, and optional cause.
type AppError struct {
	Kind    Kind
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

// Unwrap supports errors.Is / errors.As.
func (e *AppError) Unwrap() error { return e.Cause }

// New constructs a new AppError without a cause.
func New(kind Kind, msg string) *AppError {
	return &AppError{Kind: kind, Message: msg}
}

// Wrapf wraps an existing error with a formatted message.
func Wrapf(kind Kind, cause error, format string, args ...interface{}) *AppError {
	return &AppError{Kind: kind, Message: fmt.Sprintf(format, args...), Cause: cause}
}

// Wrap wraps an existing error.
func Wrap(kind Kind, cause error, msg string) *AppError {
	return &AppError{Kind: kind, Message: msg, Cause: cause}
}

// Is reports whether err (or any wrapped error) is of the given Kind.
func Is(err error, kind Kind) bool {
	var ae *AppError
	if errors.As(err, &ae) {
		return ae.Kind == kind
	}
	return false
}

// AsAppError returns the *AppError inside err, if any.
func AsAppError(err error) (*AppError, bool) {
	var ae *AppError
	if errors.As(err, &ae) {
		return ae, true
	}
	return nil, false
}

// Common sentinel errors (use errors.Is).
var (
	ErrNotFound      = New(KindNotFound, "resource not found")
	ErrUnauthorized  = New(KindUnauthorized, "authentication required")
	ErrForbidden     = New(KindForbidden, "forbidden")
	ErrConflict      = New(KindConflict, "conflict")
	ErrValidation    = New(KindValidation, "validation failed")
	ErrRateLimited   = New(KindRateLimited, "too many requests")
	ErrStorage       = New(KindStorage, "object storage error")
	ErrDatabase      = New(KindDatabase, "database error")
	ErrQueue         = New(KindQueue, "queue error")
	ErrMLUnavailable = New(KindInference, "ML service unavailable")
)
