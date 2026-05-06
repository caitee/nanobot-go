package errors

import (
	stderrors "errors"
	"fmt"
)

// FormatUserMessage renders a user-readable error message for common failures.
func FormatUserMessage(err error) string {
	if err == nil {
		return ""
	}

	if IsAPIKeyMissing(err) {
		structured := asStructured(err)
		provider := contextString(structured, "provider")
		if provider != "" {
			return fmt.Sprintf("The %s API key is missing. Please configure the API key and try again.", provider)
		}
		return "An API key is missing. Please configure the provider API key and try again."
	}

	if IsContextOverflow(err) {
		return "The conversation exceeded the model context limit. Try a shorter prompt or reduce prior context and retry."
	}

	if IsToolExecutionTimeout(err) {
		structured := asStructured(err)
		toolName := contextString(structured, "tool")
		if toolName != "" {
			return fmt.Sprintf("The %s tool timed out. Try again or use a simpler request.", toolName)
		}
		return "A tool execution timed out. Try again or use a simpler request."
	}

	structured := asStructured(err)
	if structured != nil && structured.Message != "" {
		return structured.Message
	}

	return err.Error()
}

func asStructured(err error) *Error {
	if err == nil {
		return nil
	}
	var structured *Error
	if stderrors.As(err, &structured) {
		return structured
	}
	return nil
}

func contextString(err *Error, key string) string {
	if err == nil || err.Context == nil {
		return ""
	}
	value, ok := err.Context[key]
	if !ok {
		return ""
	}
	return fmt.Sprint(value)
}
