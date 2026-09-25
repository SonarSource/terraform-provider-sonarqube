package client

import (
	"net/http/httptest"
	"testing"
)

const dopApplicationsAnswer = `{
  "devOpsPlatformApplications": [
    {
      "id": "github#sonarqube-cloud-dev11",
      "devOpsPlatform": "github",
      "applicationKey": "sonarqube-cloud-dev11",
      "bindingType": "integration-dop"
    }
  ]
}`

func TestListDopApplications(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := httptest.NewServer(rec.serve(dopApplicationsAnswer))
	defer srv.Close()

	applications, err := newTestClient(srv).ListDopApplications(t.Context(), PlatformGitHub)
	if err != nil {
		t.Fatalf("ListDopApplications() returned %v", err)
	}

	if want := "/dop-translation/dop-applications"; rec.path != want {
		t.Errorf("path = %q, want %q", rec.path, want)
	}
	if want := "devOpsPlatform=github"; rec.query != want {
		t.Errorf("query = %q, want %q", rec.query, want)
	}

	if len(applications) != 1 {
		t.Fatalf("got %d applications, want 1", len(applications))
	}
	if got, want := applications[0].ApplicationKey, "sonarqube-cloud-dev11"; got != want {
		t.Errorf("ApplicationKey = %q, want %q", got, want)
	}
}

// An empty platform asks for every application, so no parameter goes with the
// request.
func TestListDopApplicationsOfEveryPlatform(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := httptest.NewServer(rec.serve(dopApplicationsAnswer))
	defer srv.Close()

	if _, err := newTestClient(srv).ListDopApplications(t.Context(), ""); err != nil {
		t.Fatalf("ListDopApplications() returned %v", err)
	}

	if rec.query != "" {
		t.Errorf("query = %q, want it empty", rec.query)
	}
}
