package errors

// Category describes the subsystem that produced an error.
type Category string

const (
	CategoryProvider Category = "provider"
	CategoryTool     Category = "tool"
	CategoryRuntime  Category = "runtime"
	CategoryConfig   Category = "config"
	CategoryPlugin   Category = "plugin"
)

// Severity describes the impact level of an error.
type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// Error is the structured error type used across the application.
type Error struct {
	Category    Category
	Severity    Severity
	Code        Code
	Message     string
	Cause       error
	Context     map[string]any
	Recoverable bool
}

// Error returns the user-facing message for the structured error.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return string(e.Code)
}

// Unwrap exposes the underlying cause for errors.Is and errors.As.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// New constructs a structured error without an underlying cause.
func New(category Category, severity Severity, code Code, message string) *Error {
	return &Error{
		Category: category,
		Severity: severity,
		Code:     code,
		Message:  message,
	}
}

// Wrap constructs a structured error with an underlying cause and metadata.
func Wrap(cause error, category Category, severity Severity, code Code, message string, context map[string]any, recoverable bool) *Error {
	return &Error{
		Category:    category,
		Severity:    severity,
		Code:        code,
		Message:     message,
		Cause:       cause,
		Context:     context,
		Recoverable: recoverable,
	}
}
