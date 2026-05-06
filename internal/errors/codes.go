package errors

import stderrors "errors"

// Code identifies a structured error condition.
type Code string

const (
	CodeProviderAPIKeyMissing Code = "provider.api_key_missing"
	CodeProviderRequestFailed Code = "provider.request_failed"

	CodeToolExecutionTimeout Code = "tool.execution_timeout"
	CodeToolNotFound         Code = "tool.not_found"

	CodeRuntimeContextOverflow Code = "runtime.context_overflow"
	CodeRuntimeInternalError   Code = "runtime.internal_error"

	CodeConfigInvalid        Code = "config.invalid"
	CodeConfigMissingValue   Code = "config.missing_value"

	CodePluginLoadFailed     Code = "plugin.load_failed"
	CodePluginExecutionError Code = "plugin.execution_error"
)

// IsContextOverflow reports whether err is a structured context overflow error.
func IsContextOverflow(err error) bool {
	return hasCode(err, CodeRuntimeContextOverflow)
}

// IsAPIKeyMissing reports whether err is a structured missing API key error.
func IsAPIKeyMissing(err error) bool {
	return hasCode(err, CodeProviderAPIKeyMissing)
}

// IsToolExecutionTimeout reports whether err is a structured tool timeout error.
func IsToolExecutionTimeout(err error) bool {
	return hasCode(err, CodeToolExecutionTimeout)
}

func hasCode(err error, code Code) bool {
	var structured *Error
	if !stderrors.As(err, &structured) {
		return false
	}
	return structured != nil && structured.Code == code
}
