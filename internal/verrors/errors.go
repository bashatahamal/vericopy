package verrors

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Code is a stable, machine-readable diagnostic identifier.
type Code string

const (
	CodeInvalidArguments          Code = "INVALID_ARGUMENTS"
	CodeInvalidLocalPath          Code = "INVALID_LOCAL_PATH"
	CodeUnsupportedFileType       Code = "UNSUPPORTED_FILE_TYPE"
	CodeInvalidRemoteDestination  Code = "INVALID_REMOTE_DESTINATION"
	CodeSourcePathDialectMismatch Code = "SOURCE_PATH_DIALECT_MISMATCH"
	CodeKnownHostsUnavailable     Code = "KNOWN_HOSTS_UNAVAILABLE"
	CodeHostKeyRejected           Code = "HOST_KEY_REJECTED"
	CodeAuthenticationFailed      Code = "AUTHENTICATION_FAILED"
	CodeConnectionFailed          Code = "CONNECTION_FAILED"
	CodeDestinationExists         Code = "DESTINATION_EXISTS"
	CodeDestinationNotWritable    Code = "DESTINATION_NOT_WRITABLE"
	CodeDestinationNotReadable    Code = "DESTINATION_NOT_READABLE_BY_SERVICE"
	CodeResumeIncompatible        Code = "RESUME_INCOMPATIBLE"
	CodeSourceChanged             Code = "SOURCE_CHANGED_DURING_TRANSFER"
	CodeChecksumMismatch          Code = "CHECKSUM_MISMATCH"
	CodeInvalidPermission         Code = "INVALID_PERMISSION"
	CodeGroupUnavailable          Code = "GROUP_UNAVAILABLE"
	CodeTransferInterrupted       Code = "TRANSFER_INTERRUPTED"
	CodeTransferFailed            Code = "TRANSFER_FAILED"
	CodeVerificationFailed        Code = "VERIFICATION_FAILED"
	CodeBackendUnavailable        Code = "BACKEND_UNAVAILABLE"
	CodeInternal                  Code = "INTERNAL_ERROR"
)

// Error carries safe user-facing context while retaining an internal cause.
type Error struct {
	Code    Code           `json:"code"`
	Message string         `json:"message"`
	Hint    string         `json:"hint,omitempty"`
	Details map[string]any `json:"details,omitempty"`
	cause   error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.cause }

// New creates a diagnostic without an underlying cause.
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// Wrap creates a diagnostic and retains the cause without serializing it.
func Wrap(code Code, message string, cause error) *Error {
	return &Error{Code: code, Message: message, cause: cause}
}

// WithHint returns the same error with remediation guidance.
func (e *Error) WithHint(hint string) *Error {
	e.Hint = hint
	return e
}

// WithDetails returns the same error with non-secret structured context.
func (e *Error) WithDetails(details map[string]any) *Error {
	e.Details = details
	return e
}

// As converts any error to the public diagnostic shape.
func As(err error) *Error {
	if err == nil {
		return nil
	}
	var diagnostic *Error
	if errors.As(err, &diagnostic) {
		return diagnostic
	}
	return Wrap(CodeInternal, "an unexpected error occurred", err)
}

// ExitStatus maps diagnostics to stable process exit categories.
func ExitStatus(err error) int {
	switch As(err).Code {
	case CodeInvalidArguments, CodeInvalidLocalPath, CodeInvalidRemoteDestination,
		CodeUnsupportedFileType, CodeInvalidPermission, CodeSourcePathDialectMismatch:
		return 2
	case CodeKnownHostsUnavailable, CodeHostKeyRejected, CodeAuthenticationFailed:
		return 3
	case CodeChecksumMismatch, CodeVerificationFailed, CodeSourceChanged, CodeResumeIncompatible:
		return 5
	case CodeDestinationNotReadable, CodeGroupUnavailable:
		return 6
	case CodeInternal:
		return 10
	default:
		return 4
	}
}

// MarshalJSON deliberately omits the internal cause.
func (e *Error) MarshalJSON() ([]byte, error) {
	type publicError struct {
		Code    Code           `json:"code"`
		Message string         `json:"message"`
		Hint    string         `json:"hint,omitempty"`
		Details map[string]any `json:"details,omitempty"`
	}
	return json.Marshal(publicError{Code: e.Code, Message: e.Message, Hint: e.Hint, Details: e.Details})
}
