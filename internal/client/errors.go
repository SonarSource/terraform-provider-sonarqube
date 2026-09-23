package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrNotFound reports that an entity does not exist. Web API v2 answers a
// missing organization with a 404, and an answer that carries nothing becomes
// the same error, so a caller has one condition to test.
var ErrNotFound = errors.New("not found")

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
