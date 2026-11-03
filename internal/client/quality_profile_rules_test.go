package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestQualityProfileRuleWritesSendForm(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		call func(ctx context.Context, c *Client) error
		want url.Values
		path string
	}{
		"activate with values": {
			call: func(ctx context.Context, c *Client) error {
				return c.ActivateQualityProfileRule(ctx, "profile-1", "java:S107", RuleValues{
					Impacts: map[string]string{"MAINTAINABILITY": "HIGH", "RELIABILITY": "LOW"},
					Params:  map[string]string{"max": "5", "format": "^[a-z];x$"},
				})
			},
			path: "activate_rule",
			want: url.Values{"key": {"profile-1"}, "rule": {"java:S107"},
				"impacts": {`MAINTAINABILITY="HIGH";RELIABILITY="LOW"`},
				"params":  {`format="^[a-z];x$";max="5"`}},
		},
		"activate without values": {
			call: func(ctx context.Context, c *Client) error {
				return c.ActivateQualityProfileRule(ctx, "profile-1", "java:S107", RuleValues{Params: map[string]string{}})
			},
			path: "activate_rule",
			want: url.Values{"key": {"profile-1"}, "rule": {"java:S107"}},
		},
		"reset": {
			call: func(ctx context.Context, c *Client) error {
				return c.ResetQualityProfileRule(ctx, "profile-1", "java:S107")
			},
			path: "activate_rule",
			want: url.Values{"key": {"profile-1"}, "rule": {"java:S107"}, "reset": {"true"}},
		},
		"deactivate": {
			call: func(ctx context.Context, c *Client) error {
				return c.DeactivateQualityProfileRule(ctx, "profile-1", "java:S107")
			},
			path: "deactivate_rule",
			want: url.Values{"key": {"profile-1"}, "rule": {"java:S107"}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var requests []string
			if err := tt.call(t.Context(), profileServer(t, http.StatusNoContent, "", &requests)); err != nil {
				t.Fatal(err)
			}
			want := "POST /api/qualityprofiles/" + tt.path + " " + tt.want.Encode()
			if !reflect.DeepEqual(requests, []string{want}) {
				t.Errorf("requests = %v, want %s", requests, want)
			}
		})
	}
}

func TestDeactivateQualityProfileRuleAcceptsMissingProfile(t *testing.T) {
	t.Parallel()
	var requests []string
	c := profileServer(t, http.StatusNotFound, `{"errors":[{"msg":"Quality Profile with key 'profile-1' does not exist"}]}`, &requests)
	if err := c.DeactivateQualityProfileRule(t.Context(), "profile-1", "java:S107"); err != nil {
		t.Fatal(err)
	}
}

func TestRuleActivationExplainsRefusals(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		status int
		msg    string
		want   string
	}{
		"built-in": {http.StatusBadRequest, "Operation forbidden for built-in Quality Profile 'Sonar way' with language 'java'",
			"Create a sonarqube_quality_profile that extends it"},
		"misra": {http.StatusForbidden, "Enterprise MISRA rules require a MISRA entitlement",
			"does not have the MISRA entitlement"},
		"other": {http.StatusBadRequest, "Value 'abc' must be an integer.", "status 400: Value 'abc' must be an integer."},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var requests []string
			c := profileServer(t, tt.status, `{"errors":[{"msg":"`+tt.msg+`"}]}`, &requests)
			err := c.ActivateQualityProfileRule(t.Context(), "profile-1", "java:S107", RuleValues{})
			var apiErr *APIError
			if err == nil || !strings.Contains(err.Error(), tt.want) || !errors.As(err, &apiErr) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestGetActiveRuleReadsActivationOfProfile(t *testing.T) {
	t.Parallel()
	var requests []string
	c := profileServer(t, http.StatusOK, `{"total":1,"rules":[{"key":"java:S107",
		"params":[{"key":"max","defaultValue":"7"},{"key":"constructorMax","defaultValue":"7"}],
		"impacts":[{"softwareQuality":"MAINTAINABILITY","severity":"MEDIUM"}]}],
		"actives":{"java:S107":[
			{"qProfile":"other","inherit":"NONE","params":[],"impacts":[]},
			{"qProfile":"profile-1","inherit":"OVERRIDES","params":[{"key":"max","value":"5"},{"key":"constructorMax","value":"7"}],
			 "impacts":[{"softwareQuality":"MAINTAINABILITY","severity":"HIGH"}]}]}}`, &requests)
	active, err := c.GetActiveRule(t.Context(), "profile-1", "java:S107")
	if err != nil {
		t.Fatal(err)
	}
	want := ActiveRule{
		Inherit: RuleInheritOverrides,
		Values: RuleValues{
			Impacts: map[string]string{"MAINTAINABILITY": "HIGH"},
			Params:  map[string]string{"max": "5", "constructorMax": "7"},
		},
		Defaults: RuleValues{
			Impacts: map[string]string{"MAINTAINABILITY": "MEDIUM"},
			Params:  map[string]string{"max": "7", "constructorMax": "7"},
		},
	}
	if !reflect.DeepEqual(*active, want) {
		t.Errorf("active = %+v", *active)
	}
	query := url.Values{"qprofile": {"profile-1"}, "activation": {"true"}, "rule_key": {"java:S107"}, "f": {"actives,params,impacts"}}
	if requests[0] != "GET /api/rules/search "+query.Encode() {
		t.Errorf("request = %s", requests[0])
	}
}

func TestGetActiveRuleNotFound(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		status int
		body   string
	}{
		"not active":      {http.StatusOK, `{"total":0,"rules":[],"actives":{}}`},
		"other profile":   {http.StatusOK, `{"total":1,"rules":[],"actives":{"java:S107":[{"qProfile":"other"}]}}`},
		"missing profile": {http.StatusNotFound, `{"errors":[{"msg":"The specified qualityProfile 'profile-1' does not exist"}]}`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var requests []string
			_, err := profileServer(t, tt.status, tt.body, &requests).GetActiveRule(t.Context(), "profile-1", "java:S107")
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
