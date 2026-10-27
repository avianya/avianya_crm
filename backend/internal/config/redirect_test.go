package config

import "testing"

func TestCheckRedirectURL(t *testing.T) {
	ok := []string{
		"http://localhost:8080/api/integrations/google-sheets/callback",
		"https://crm.example.com/api/integrations/google-sheets/callback",
	}
	for _, u := range ok {
		if err := checkRedirectURL(u); err != nil {
			t.Errorf("%s: %v", u, err)
		}
	}
	bad := []string{
		"",
		"localhost:8080/api/integrations/google-sheets/callback",
		"ftp://localhost/api/integrations/google-sheets/callback",
		"https://api.avianya.com/oauth/google/sheets/redirect",
		"http://localhost:8080/api/integrations/google-sheets/callback/",
		"http://localhost:8080/api/integrations/google-sheets/callback?x=1",
	}
	for _, u := range bad {
		if err := checkRedirectURL(u); err == nil {
			t.Errorf("%q was accepted", u)
		}
	}
}
