package runtime

import (
	stderrors "errors"
	"fmt"
	"strings"

	"nanobot-go/internal/errors"
)

// mapProviderError maps provider-related errors to structured errors.
// It inspects the error message to determine the appropriate error code.
func mapProviderError(err error, errorMessage string) error {
	if err == nil && errorMessage == "" {
		return nil
	}

	msg := errorMessage
	if msg == "" && err != nil {
		msg = err.Error()
	}

	// Check for API key missing errors
	if strings.Contains(strings.ToLower(msg), "api key") {
		provider := extractProvider(msg)
		ctx := map[string]any{}
		if provider != "" {
			ctx["provider"] = provider
		}
		return errors.Wrap(
			err,
			errors.CategoryProvider,
			errors.SeverityError,
			errors.CodeProviderAPIKeyMissing,
			"Provider API key is missing",
			ctx,
			false,
		)
	}

	// Default to provider request failed
	return errors.Wrap(
		err,
		errors.CategoryProvider,
		errors.SeverityError,
		errors.CodeProviderRequestFailed,
		"Provider request failed: "+msg,
		nil,
		true,
	)
}

// mapRuntimeError maps runtime-related errors to structured errors.
func mapRuntimeError(err error, operation string) error {
	if err == nil {
		return nil
	}

	// Check if already structured
	var structuredErr *errors.Error
	if stderrors.As(err, &structuredErr) {
		return err
	}

	ctx := map[string]any{}
	if operation != "" {
		ctx["operation"] = operation
	}

	return errors.Wrap(
		err,
		errors.CategoryRuntime,
		errors.SeverityError,
		errors.CodeRuntimeInternalError,
		fmt.Sprintf("Runtime error in %s: %v", operation, err),
		ctx,
		false,
	)
}

// mapGetAPIKeyError maps getAPIKey hook errors to structured errors.
func mapGetAPIKeyError(err error, provider string) error {
	if err == nil {
		return nil
	}

	ctx := map[string]any{}
	if provider != "" {
		ctx["provider"] = provider
	}

	return errors.Wrap(
		err,
		errors.CategoryProvider,
		errors.SeverityError,
		errors.CodeProviderAPIKeyMissing,
		"Failed to retrieve API key",
		ctx,
		false,
	)
}

// extractProvider attempts to extract provider name from error message.
func extractProvider(msg string) string {
	lower := strings.ToLower(msg)
	providers := []string{"anthropic", "openai", "google", "azure"}
	for _, p := range providers {
		if strings.Contains(lower, p) {
			return p
		}
	}
	return ""
}
