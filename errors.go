package core

import (
	"errors"
	"fmt"
)

var (
	// ErrMissingAPIKey is returned when no API key is provided.
	ErrMissingAPIKey = errors.New("API key is required")

	// ErrNoChoices is returned when the API response has no choices.
	ErrNoChoices = errors.New("no choices in API response")

	// ErrToolCancelled is returned when tool execution is cancelled by user.
	ErrToolCancelled = errors.New("tool execution cancelled by user")

	// ErrStringNotFound is returned when the target string is not found in edit_file.
	ErrStringNotFound = errors.New("old_string not found in file")

	// ErrMultipleMatches is returned when multiple matches are found without replace_all.
	ErrMultipleMatches = errors.New("multiple matches found, use replace_all")

	// ErrSandboxBlocked is returned when sandbox policy blocks command execution.
	ErrSandboxBlocked = errors.New("command blocked by sandbox policy")
)

// APIError represents an error from the chat API.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error (%d): %s", e.StatusCode, e.Body)
}
