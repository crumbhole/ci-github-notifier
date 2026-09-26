package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/imroc/req/v3"
)

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

// startAppAuth starts a stub GitHub and returns a client for it, the
// host to use as gh_url, and App credentials to authenticate with.
func startAppAuth(t *testing.T, h http.Handler) (*req.Client, string, credentials) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)

	key, _ := testKeyPEM(t)
	creds := credentials{appID: "12345", appKey: key}

	return req.C().EnableInsecureSkipVerify(), strings.TrimPrefix(srv.URL, "https://"), creds
}

func TestInstallationTokenExchangesAppJWTForInstallationToken(t *testing.T) {
	stub := &appAuthTestServer{}
	client, host, creds := startAppAuth(t, stub)

	token, err := installationToken(client, creds, host, "crumbhole", "ci-github-notifier", map[string]string{"statuses": "write"})

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
	client, host, creds := startAppAuth(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	_, err := installationToken(client, creds, host, "crumbhole", "ci-github-notifier", map[string]string{"statuses": "write"})

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
	client, host, creds := startAppAuth(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/installation") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id": 42}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))

	_, err := installationToken(client, creds, host, "crumbhole", "ci-github-notifier", map[string]string{"statuses": "write"})

	if err == nil {
		t.Fatal("installationToken() = nil error, want an error when GitHub rejects the App JWT")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error %q does not carry GitHub's status, leaving nothing to diagnose from", err)
	}
}

func TestInstallationTokenNarrowsScopeToRepoAndPermissions(t *testing.T) {
	stub := &appAuthTestServer{}
	client, host, creds := startAppAuth(t, stub)

	_, err := installationToken(client, creds, host, "crumbhole", "ci-github-notifier",
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
