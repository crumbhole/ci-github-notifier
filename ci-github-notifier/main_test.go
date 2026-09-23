package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/imroc/req/v3"
)

func TestResolveTokenMintsInstallationTokenWhenAppConfigured(t *testing.T) {
	client, host := startAppAuth(t, &appAuthTestServer{})
	t.Setenv("access_token", "")
	t.Setenv("tokenFile", "")

	token, prefix, err := resolveToken(client, host, "crumbhole", "ci-github-notifier", apiStatuses)

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

func TestResolveTokenFallsBackToAccessToken(t *testing.T) {
	t.Setenv("app_id", "")
	t.Setenv("app_private_key", "")
	t.Setenv("app_private_key_file", "")
	t.Setenv("access_token", "ghp_classicpat")
	t.Setenv("tokenFile", "")

	token, prefix, err := resolveToken(req.C(), "api.github.com", "crumbhole", "ci-github-notifier", apiStatuses)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "ghp_classicpat" {
		t.Errorf("token = %q, want the access_token value", token)
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
	t.Setenv("app_id", "")
	t.Setenv("app_private_key", "")
	t.Setenv("app_private_key_file", "")
	t.Setenv("access_token", signed)
	t.Setenv("tokenFile", "")

	_, prefix, err := resolveToken(req.C(), "api.github.com", "crumbhole", "ci-github-notifier", apiStatuses)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prefix != "Bearer" {
		t.Errorf("prefix = %q, want Bearer for a JWT access_token", prefix)
	}
}

func TestResolveTokenRefusesPartialAppConfig(t *testing.T) {
	t.Setenv("app_id", "12345")
	t.Setenv("app_private_key", "")
	t.Setenv("app_private_key_file", "")
	t.Setenv("access_token", "ghp_classicpat")
	t.Setenv("tokenFile", "")

	_, _, err := resolveToken(req.C(), "api.github.com", "crumbhole", "ci-github-notifier", apiStatuses)

	if err == nil {
		t.Fatal("resolveToken() = nil error, want a refusal rather than a silent fall back to access_token")
	}
}

func TestResolveTokenPrefersAccessTokenOverTokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("token-from-file"), 0o600); err != nil {
		t.Fatalf("writing token file: %v", err)
	}
	t.Setenv("app_id", "")
	t.Setenv("app_private_key", "")
	t.Setenv("app_private_key_file", "")
	t.Setenv("access_token", "token-from-env")
	t.Setenv("tokenFile", path)

	got, _, err := resolveToken(req.C(), "api.github.com", "crumbhole", "ci-github-notifier", apiStatuses)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "token-from-env" {
		t.Errorf("token = %q, want access_token to win over tokenFile", got)
	}
}
