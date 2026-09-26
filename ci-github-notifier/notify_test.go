package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/imroc/req/v3"
)

func testNotification() notification {
	return notification{
		state:        "success",
		targetURL:    "https://ci.example.com/run/1",
		description:  "Build passed",
		context:      "ci/build",
		apiHost:      "api.github.com",
		organisation: "crumbhole",
		repo:         "ci-github-notifier",
		sha:          "123abc",
		api:          apiStatuses,
	}
}

func TestResolveTokenMintsInstallationTokenWhenAppConfigured(t *testing.T) {
	client, host, creds := startAppAuth(t, &appAuthTestServer{})
	n := testNotification()
	n.apiHost = host

	token, prefix, err := resolveToken(client, n, creds)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "ghs_installationtoken" {
		t.Errorf("token = %q, want the minted installation token", token)
	}
	if prefix != "Bearer" {
		t.Errorf("prefix = %q, want Bearer for an installation token", prefix)
	}
}

func TestResolveTokenUsesAccessToken(t *testing.T) {
	token, prefix, err := resolveToken(req.C(), testNotification(), credentials{accessToken: "ghp_classicpat"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "ghp_classicpat" {
		t.Errorf("token = %q, want the access token", token)
	}
	if prefix != "token" {
		t.Errorf("prefix = %q, want token for a PAT", prefix)
	}
}

func TestResolveTokenKeepsBearerPrefixForJWTAccessToken(t *testing.T) {
	key, _ := testKeyPEM(t)
	signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{Issuer: "12345"}).SignedString(key)
	if err != nil {
		t.Fatalf("signing test token: %v", err)
	}

	_, prefix, err := resolveToken(req.C(), testNotification(), credentials{accessToken: signed})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prefix != "Bearer" {
		t.Errorf("prefix = %q, want Bearer for a JWT access_token", prefix)
	}
}

// statusRecorder captures the commit status request GitHub would receive.
type statusRecorder struct {
	body   map[string]string
	path   string
	auth   string
	status int
}

func (s *statusRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.path = r.URL.Path
	s.auth = r.Header.Get("Authorization")
	if err := json.NewDecoder(r.Body).Decode(&s.body); err != nil {
		s.body = map[string]string{"decode error": err.Error()}
	}
	w.WriteHeader(s.status)
}

func TestNotifyPostsCommitStatus(t *testing.T) {
	rec := &statusRecorder{status: http.StatusCreated}
	srv := httptest.NewTLSServer(rec)
	t.Cleanup(srv.Close)
	n := testNotification()
	n.apiHost = strings.TrimPrefix(srv.URL, "https://")

	id, err := notify(req.C().EnableInsecureSkipVerify(), n, credentials{accessToken: "ghp_classicpat"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 0 {
		t.Errorf("id = %d, want 0 as statuses have no id to thread", id)
	}
	if rec.path != "/repos/crumbhole/ci-github-notifier/statuses/123abc" {
		t.Errorf("path = %s, want the commit's statuses", rec.path)
	}
	if rec.auth != "token ghp_classicpat" {
		t.Errorf("Authorization = %q, want the access token", rec.auth)
	}
	want := map[string]string{
		"state":       "success",
		"target_url":  "https://ci.example.com/run/1",
		"description": "Build passed",
		"context":     "ci/build",
	}
	for k, v := range want {
		if rec.body[k] != v {
			t.Errorf("body[%q] = %q, want %q", k, rec.body[k], v)
		}
	}
}

func TestNotifyReportsRejectedStatus(t *testing.T) {
	rec := &statusRecorder{status: http.StatusUnprocessableEntity}
	srv := httptest.NewTLSServer(rec)
	t.Cleanup(srv.Close)
	n := testNotification()
	n.apiHost = strings.TrimPrefix(srv.URL, "https://")

	_, err := notify(req.C().EnableInsecureSkipVerify(), n, credentials{accessToken: "ghp_classicpat"})

	if err == nil {
		t.Fatal("notify() = nil error, want an error when GitHub rejects the status")
	}
}

func TestNotifyRefusesChecksWithoutAppBeforeCallingGitHub(t *testing.T) {
	rec := &statusRecorder{status: http.StatusCreated}
	srv := httptest.NewTLSServer(rec)
	t.Cleanup(srv.Close)
	n := testNotification()
	n.apiHost = strings.TrimPrefix(srv.URL, "https://")
	n.api = apiChecks

	_, err := notify(req.C().EnableInsecureSkipVerify(), n, credentials{accessToken: "ghp_classicpat"})

	if err == nil {
		t.Fatal("notify() = nil error, want a refusal: a PAT cannot create check runs")
	}
	if rec.path != "" {
		t.Errorf("GitHub was called at %s, want the refusal before any request", rec.path)
	}
}
