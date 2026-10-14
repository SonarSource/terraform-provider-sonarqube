package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrNotFound reports that an entity does not exist. Web API v2 answers a
// missing organization with a 404, and an answer that carries nothing becomes
// the same error, so a caller has one condition to test.
var ErrNotFound = errors.New("not found")

// ErrOrganizationNotFound reports that a search inside an organization
// answered 404. The organization does not exist, or the token cannot read it.
// It does not match ErrNotFound, so that a caller can tell it apart from an
// entity that the organization does not hold.
var ErrOrganizationNotFound = errors.New("organization not found")

// ErrProjectNotFound reports that a call inside a project answered 404 because
// the project does not exist, or the token cannot read it. It does not match
// ErrNotFound or ErrOrganizationNotFound.
var ErrProjectNotFound = errors.New("project not found")

// Scope kinds of a ScopeNotFoundError.
const (
	ScopeOrganization = "organization"
	ScopeProject      = "project"
)

// ScopeNotFoundError reports which part of a scope answered 404. It matches
// ErrOrganizationNotFound or ErrProjectNotFound, after its Kind, and not
// ErrNotFound.
type ScopeNotFoundError struct {
	// Kind is ScopeOrganization or ScopeProject.
	Kind string
	// Key is the key of the organization or of the project.
	Key string
	// Cause is the error of the call.
	Cause error
}

func (e *ScopeNotFoundError) Error() string {
	return fmt.Sprintf("%s not found: %q: %s", e.Kind, e.Key, e.Cause)
}

// Is makes errors.Is match the sentinel of the kind.
func (e *ScopeNotFoundError) Is(target error) bool {
	switch target {
	case ErrOrganizationNotFound:
		return e.Kind == ScopeOrganization
	case ErrProjectNotFound:
		return e.Kind == ScopeProject
	default:
		return false
	}
}

// APIError is a response with a status outside the 2xx range.
type APIError struct {
	StatusCode int
	Messages   []string
	Body       string
}

// Error returns the status and the messages of the response.
func (e *APIError) Error() string {
	detail := strings.Join(e.Messages, "; ")
	if detail == "" {
		detail = e.Body
	}
	if detail == "" {
		return fmt.Sprintf("unexpected status %d from the API", e.StatusCode)
	}
	return fmt.Sprintf("status %d: %s", e.StatusCode, detail)
}

// Is makes errors.Is(err, ErrNotFound) true for a 404.
func (e *APIError) Is(target error) bool {
	return errors.Is(target, ErrNotFound) && e.StatusCode == 404
}

// newAPIError reads the messages out of an error body. The web service answers
// {"errors":[{"msg":"..."}]}, the REST API answers {"message":"..."}, and a body
// in neither shape stays available in Body.
func newAPIError(status int, body []byte) *APIError {
	apiErr := &APIError{StatusCode: status, Body: string(body)}

	var envelope struct {
		Errors []struct {
			Msg string `json:"msg"`
		} `json:"errors"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil {
		for _, e := range envelope.Errors {
			apiErr.Messages = append(apiErr.Messages, e.Msg)
		}
		if envelope.Message != "" {
			apiErr.Messages = append(apiErr.Messages, envelope.Message)
		}
	}
	return apiErr
}

// hasMessagePrefix reports a message of an API error that starts with prefix.
func hasMessagePrefix(messages []string, prefix string) bool {
	return slices.ContainsFunc(messages, func(message string) bool { return strings.HasPrefix(message, prefix) })
}

// hasMessageContaining reports a message of an API error that holds part.
func hasMessageContaining(messages []string, part string) bool {
	return slices.ContainsFunc(messages, func(message string) bool { return strings.Contains(message, part) })
}
