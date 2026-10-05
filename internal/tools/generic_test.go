package tools

import "testing"

func TestAPIPath(t *testing.T) {
	ok := map[string]string{
		"/api/v2/services/dlp/profiles": "/api/v2/services/dlp/profiles",
		"api/v2/ips/status/":            "/api/v2/ips/status",
	}
	for in, want := range ok {
		if got, err := apiPath(in); err != nil || got != want {
			t.Errorf("apiPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"/api/v1/whatever",
		"/api/v2/../v1/x",
		"/api/v2/events/dataexport/events/page", // SIEM cursor
		"/api/v2/vpe/tokens",                    // credentials
		"/api/v2/streamingclient/clients/1/token",
		"/api/v2/steering/securerepo/tokens",
		"/api/v2/devices/otp",
		"/api/v2/enrollment/tokenset",
	} {
		if _, err := apiPath(in); err == nil {
			t.Errorf("apiPath(%q) accepted, want refused", in)
		}
	}
}

func TestAPIRequestMethods(t *testing.T) {
	for _, c := range []struct{ method, path string }{
		{"", "/api/v2/ips/status"},
		{"post", "/api/v2/users/getusers"},
		{"POST", "/api/v2/adem/users/device/getdetails"},
	} {
		if _, _, err := apiRequest(c.method, c.path); err != nil {
			t.Errorf("%s %s refused: %v", c.method, c.path, err)
		}
	}
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/v2/profiles/destinations"}, // a create
		{"POST", "/api/v2/devices/getclientlogs"}, // makes clients upload logs
		{"PATCH", "/api/v2/ips/getsignaturelist"}, // only POST lookups
		{"DELETE", "/api/v2/incidents/watchlists/1"},
		{"POST", "/api/v2/events/dataexport/x/getthing"}, // path refusals still apply
	} {
		if _, _, err := apiRequest(c.method, c.path); err == nil {
			t.Errorf("%s %s accepted, want refused", c.method, c.path)
		}
	}
}
