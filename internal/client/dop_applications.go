package client

import (
	"context"
	"net/url"
)

// DopApplication is an application that the instance owns on a DevOps
// platform.
//
// A binding to github.com accepts an installation of one of these only,
// because the server looks an installation up in its own records instead of
// asking GitHub. An installation of any other GitHub application is
// therefore unknown to the instance, however correct its identifier looks.
type DopApplication struct {
	ID             string `json:"id"`
	DevOpsPlatform string `json:"devOpsPlatform"`
	ApplicationKey string `json:"applicationKey"`
	BindingType    string `json:"bindingType"`
}

// ListDopApplications returns the applications of the instance. An empty
// devOpsPlatform asks for all of them.
func (c *Client) ListDopApplications(ctx context.Context, devOpsPlatform string) ([]DopApplication, error) {
	params := url.Values{}
	if devOpsPlatform != "" {
		params.Set("devOpsPlatform", devOpsPlatform)
	}

	var out struct {
		DevOpsPlatformApplications []DopApplication `json:"devOpsPlatformApplications"`
	}
	if err := c.apiGet(ctx, "/dop-translation/dop-applications", params, &out); err != nil {
		return nil, err
	}
	return out.DevOpsPlatformApplications, nil
}
