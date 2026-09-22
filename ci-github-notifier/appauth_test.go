package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/imroc/req/v3"
)

func TestAppAuthConfiguredFalseWhenNothingSet(t *testing.T) {
	t.Setenv("app_id", "")
	t.Setenv("app_private_key", "")
	t.Setenv("app_private_key_file", "")

	configured, err := appAuthConfigured()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if configured {
		t.Error("appAuthConfigured() = true, want false when no App credentials are set")
	}
}

func TestAppAuthConfiguredTrueWithIDAndKey(t *testing.T) {
	t.Setenv("app_id", "12345")
	t.Setenv("app_private_key", "cGVt")
	t.Setenv("app_private_key_file", "")

	configured, err := appAuthConfigured()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !configured {
		t.Error("appAuthConfigured() = false, want true when app_id and app_private_key are set")
	}
}

func TestAppAuthConfiguredTrueWithIDAndKeyFile(t *testing.T) {
	t.Setenv("app_id", "12345")
	t.Setenv("app_private_key", "")
	t.Setenv("app_private_key_file", "/path/to/key.pem")

	configured, err := appAuthConfigured()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !configured {
		t.Error("appAuthConfigured() = false, want true when app_id and app_private_key_file are set")
	}
}

func TestAppAuthConfiguredErrorsWhenIDSetWithoutKey(t *testing.T) {
	t.Setenv("app_id", "12345")
	t.Setenv("app_private_key", "")
	t.Setenv("app_private_key_file", "")

	_, err := appAuthConfigured()

	if err == nil {
		t.Fatal("appAuthConfigured() = nil error, want an error when app_id is set with no key")
	}
	if !strings.Contains(err.Error(), "app_private_key") {
		t.Errorf("error %q does not name the missing variable app_private_key", err)
	}
}

func TestAppAuthConfiguredErrorsWhenKeySetWithoutID(t *testing.T) {
	t.Setenv("app_id", "")
	t.Setenv("app_private_key", "cGVt")
	t.Setenv("app_private_key_file", "")

	_, err := appAuthConfigured()

	if err == nil {
		t.Fatal("appAuthConfigured() = nil error, want an error when a key is set with no app_id")
	}
	if !strings.Contains(err.Error(), "app_id") {
		t.Errorf("error %q does not name the missing variable app_id", err)
	}
}

// testKeyPEM generates a throwaway RSA key and returns it with its
// PKCS#1 PEM encoding, the form GitHub hands out for App private keys.
func testKeyPEM(t *testing.T) (*rsa.PrivateKey, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating test key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return key, pemBytes
}

func TestAppPrivateKeyFromBase64EnvVar(t *testing.T) {
	want, pemBytes := testKeyPEM(t)
	t.Setenv("app_private_key", base64.StdEncoding.EncodeToString(pemBytes))
	t.Setenv("app_private_key_file", "")

	got, err := appPrivateKey()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Equal(want) {
		t.Error("appPrivateKey() returned a different key than the one encoded in app_private_key")
	}
}

func TestAppPrivateKeyFromFile(t *testing.T) {
	want, pemBytes := testKeyPEM(t)
	path := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatalf("writing key file: %v", err)
	}
	t.Setenv("app_private_key", "")
	t.Setenv("app_private_key_file", path)

	got, err := appPrivateKey()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Equal(want) {
		t.Error("appPrivateKey() returned a different key than the one in app_private_key_file")
	}
}

func TestAppPrivateKeyPrefersEnvVarOverFile(t *testing.T) {
	want, pemBytes := testKeyPEM(t)
	_, otherPEM := testKeyPEM(t)
	path := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(path, otherPEM, 0o600); err != nil {
		t.Fatalf("writing key file: %v", err)
	}
	t.Setenv("app_private_key", base64.StdEncoding.EncodeToString(pemBytes))
	t.Setenv("app_private_key_file", path)

	got, err := appPrivateKey()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Equal(want) {
		t.Error("appPrivateKey() used app_private_key_file, want app_private_key to take precedence")
	}
}

func TestAppPrivateKeyErrorsOnNonBase64EnvVar(t *testing.T) {
	t.Setenv("app_private_key", "-----BEGIN RSA PRIVATE KEY-----")
	t.Setenv("app_private_key_file", "")

	_, err := appPrivateKey()

	if err == nil {
		t.Fatal("appPrivateKey() = nil error, want an error for a non-base64 app_private_key")
	}
	if !strings.Contains(err.Error(), "app_private_key") {
		t.Errorf("error %q does not name app_private_key, so will not tell the user what to fix", err)
	}
}

func TestAppPrivateKeyErrorsOnMalformedPEM(t *testing.T) {
	t.Setenv("app_private_key", base64.StdEncoding.EncodeToString([]byte("not a pem")))
	t.Setenv("app_private_key_file", "")

	_, err := appPrivateKey()

	if err == nil {
		t.Fatal("appPrivateKey() = nil error, want an error for a malformed PEM")
	}
}

func TestAppPrivateKeyErrorsOnMissingFile(t *testing.T) {
	t.Setenv("app_private_key", "")
	t.Setenv("app_private_key_file", filepath.Join(t.TempDir(), "absent.pem"))

	_, err := appPrivateKey()

	if err == nil {
		t.Fatal("appPrivateKey() = nil error, want an error when app_private_key_file does not exist")
	}
	if !strings.Contains(err.Error(), "app_private_key_file") {
		t.Errorf("error %q does not name app_private_key_file", err)
	}
}

func TestAppJWTIsSignedWithRS256AndCarriesAppIDAndWindow(t *testing.T) {
	key, _ := testKeyPEM(t)
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	signed, err := appJWT("12345", key, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	claims := jwt.RegisteredClaims{}
	parsed, err := jwt.ParseWithClaims(signed, &claims, func(*jwt.Token) (any, error) {
		return &key.PublicKey, nil
	}, jwt.WithTimeFunc(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("token does not verify against the App public key: %v", err)
	}
	if parsed.Method.Alg() != "RS256" {
		t.Errorf("alg = %q, want RS256 (the only algorithm GitHub accepts for App JWTs)", parsed.Method.Alg())
	}
	if claims.Issuer != "12345" {
		t.Errorf("iss = %q, want the app_id 12345", claims.Issuer)
	}
	if got := claims.IssuedAt.Time; !got.Equal(now.Add(-60 * time.Second)) {
		t.Errorf("iat = %v, want it backdated 60s to %v for clock skew", got, now.Add(-60*time.Second))
	}
	if got := claims.ExpiresAt.Time; got.After(now.Add(10 * time.Minute)) {
		t.Errorf("exp = %v, which exceeds GitHub's 10 minute ceiling from %v", got, now)
	}
	if got := claims.ExpiresAt.Time; !got.After(now) {
		t.Errorf("exp = %v, which is not in the future relative to %v", got, now)
	}
}

// appAuthTestServer stands in for the GitHub API. It records the
// requests it saw so tests can assert on paths and Authorization
// headers, and serves the two-step installation token exchange.
type appAuthTestServer struct {
	mintBody map[string]any
	paths    []string
	auths    []string
}

func (s *appAuthTestServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.paths = append(s.paths, r.URL.Path)
	s.auths = append(s.auths, r.Header.Get("Authorization"))

	switch {
	case strings.HasSuffix(r.URL.Path, "/installation"):
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id": 42}`))
	case strings.HasSuffix(r.URL.Path, "/access_tokens"):
		if err := json.NewDecoder(r.Body).Decode(&s.mintBody); err != nil {
			s.mintBody = map[string]any{"decode error": err.Error()}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"token": "ghs_installationtoken"}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// startAppAuth configures App credentials pointing at a stub GitHub and
// returns the recorder plus the host to pass as gh_url.
func startAppAuth(t *testing.T, h http.Handler) (*req.Client, string) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)

	_, pemBytes := testKeyPEM(t)
	t.Setenv("app_id", "12345")
	t.Setenv("app_private_key", base64.StdEncoding.EncodeToString(pemBytes))
	t.Setenv("app_private_key_file", "")

	return req.C().EnableInsecureSkipVerify(), strings.TrimPrefix(srv.URL, "https://")
}

func TestInstallationTokenExchangesAppJWTForInstallationToken(t *testing.T) {
	stub := &appAuthTestServer{}
	client, host := startAppAuth(t, stub)

	token, err := installationToken(client, host, "crumbhole", "ci-github-notifier", map[string]string{"statuses": "write"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "ghs_installationtoken" {
		t.Errorf("installationToken() = %q, want the token from the access_tokens response", token)
	}
	want := []string{"/repos/crumbhole/ci-github-notifier/installation", "/app/installations/42/access_tokens"}
	if !reflect.DeepEqual(stub.paths, want) {
		t.Errorf("requested paths = %v, want %v", stub.paths, want)
	}
	for i, auth := range stub.auths {
		if !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("request %d Authorization = %q, want a Bearer App JWT", i, auth)
		}
	}
}

func TestInstallationTokenReportsAppNotInstalled(t *testing.T) {
	client, host := startAppAuth(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	_, err := installationToken(client, host, "crumbhole", "ci-github-notifier", map[string]string{"statuses": "write"})

	if err == nil {
		t.Fatal("installationToken() = nil error, want an error when the App is not installed")
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("error %q does not say the App is not installed, which is the actual fix", err)
	}
	if !strings.Contains(err.Error(), "crumbhole/ci-github-notifier") {
		t.Errorf("error %q does not name the repo the App is missing from", err)
	}
}

func TestInstallationTokenReportsRejectedCredentials(t *testing.T) {
	client, host := startAppAuth(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/installation") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id": 42}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))

	_, err := installationToken(client, host, "crumbhole", "ci-github-notifier", map[string]string{"statuses": "write"})

	if err == nil {
		t.Fatal("installationToken() = nil error, want an error when GitHub rejects the App JWT")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error %q does not carry GitHub's status, leaving nothing to diagnose from", err)
	}
}

func TestInstallationTokenNarrowsScopeToRepoAndPermissions(t *testing.T) {
	stub := &appAuthTestServer{}
	client, host := startAppAuth(t, stub)

	_, err := installationToken(client, host, "crumbhole", "ci-github-notifier",
		map[string]string{"statuses": "write"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Without these, GitHub mints a token good for every repository the
	// App is installed on and every permission it holds.
	repos, ok := stub.mintBody["repositories"].([]any)
	if !ok || len(repos) != 1 || repos[0] != "ci-github-notifier" {
		t.Errorf("repositories = %v, want the short name of the target repo only", stub.mintBody["repositories"])
	}
	perms, ok := stub.mintBody["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("permissions = %v, want an object limiting the token", stub.mintBody["permissions"])
	}
	if perms["statuses"] != "write" {
		t.Errorf("permissions.statuses = %v, want write", perms["statuses"])
	}
	if len(perms) != 1 {
		t.Errorf("permissions = %v, want only what was asked for", perms)
	}
}
