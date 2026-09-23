package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrNotFound reports that an entity does not exist.
//
// The organizations web service has no action that reads one organization, so
// a missing organization shows itself in two ways: a 404 from an action that
// addresses it, and an empty result list from "search". Both become this
// error, so that a caller has only one condition to test.
var ErrNotFound = errors.New("not found")

// APIError is a response with a status outside the 2xx range.
type APIError struct {
	StatusCode int
	Messages   []string
	Body       string
}

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

// Is makes errors.Is(err, ErrNotFound) true for a 404, so that a caller does
// not have to unwrap the error to find a missing entity.
func (e *APIError) Is(target error) bool {
	return errors.Is(target, ErrNotFound) && e.StatusCode == 404
}

// newAPIError reads the messages out of an error body.
//
// The two API surfaces report a failure differently. The web service answers
// {"errors":[{"msg":"..."}]} and the REST API answers {"message":"..."}. Both
// shapes are read here, because the REST API arrives with the organization
// binding. A body in neither shape stays available in Body.
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
