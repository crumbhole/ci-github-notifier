package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestAPIModeAcceptsKnownValues(t *testing.T) {
	cases := map[string]string{
		"":         apiStatuses, // unset keeps the existing behaviour
		"statuses": apiStatuses,
		"checks":   apiChecks,
	}

	for env, want := range cases {
		t.Run(env, func(t *testing.T) {
			got, err := apiMode(env)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != want {
				t.Errorf("apiMode() = %q, want %q", got, want)
			}
		})
	}
}

func TestAPIModeRejectsUnknownValues(t *testing.T) {
	// A typo or the wrong case previously fell through to statuses mode.
	// With App auth that posted a commit status and exited 0, so the
	// mistake was invisible.
	for _, env := range []string{"check", "Checks", "CHECKS", "check-runs", "banana"} {
		t.Run(env, func(t *testing.T) {
			_, err := apiMode(env)

			if err == nil {
				t.Fatalf("apiMode() = nil error for api=%q, want a refusal rather than a silent fall back to statuses", env)
			}
			if !strings.Contains(err.Error(), env) {
				t.Errorf("error %q does not quote the offending value", err)
			}
		})
	}
}

// setNotificationEnv sets every variable a statuses notification needs.
func setNotificationEnv(t *testing.T) {
	t.Helper()
	t.Setenv("state", "success")
	t.Setenv("target_url", "https://ci.example.com/run/1")
	t.Setenv("description", "Build passed")
	t.Setenv("context", "ci/build")
	t.Setenv("organisation", "crumbhole")
	t.Setenv("app_repo", "ci-github-notifier")
	t.Setenv("git_sha", "123abc")
	t.Setenv("api", "")
	t.Setenv("check_run_id", "")
	t.Setenv("check_run_id_file", "")
}

func TestNotificationFromEnvReadsEveryField(t *testing.T) {
	setNotificationEnv(t)
	t.Setenv("gh_url", "api.mydomain.biz")
	t.Setenv("api", "checks")
	t.Setenv("check_run_id", "987654")
	t.Setenv("check_run_id_file", "/tmp/check_run_id")

	got, err := notificationFromEnv()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := notification{
		state:          "success",
		targetURL:      "https://ci.example.com/run/1",
		description:    "Build passed",
		context:        "ci/build",
		apiHost:        "api.mydomain.biz",
		organisation:   "crumbhole",
		repo:           "ci-github-notifier",
		sha:            "123abc",
		api:            apiChecks,
		checkRunID:     987654,
		checkRunIDFile: "/tmp/check_run_id",
	}
	if got != want {
		t.Errorf("notificationFromEnv() = %+v, want %+v", got, want)
	}
}

func TestNotificationFromEnvDefaultsToPublicGitHub(t *testing.T) {
	setNotificationEnv(t)
	// t.Setenv cannot unset, so restore gh_url by hand after unsetting it.
	if prev, ok := os.LookupEnv("gh_url"); ok {
		t.Cleanup(func() { _ = os.Setenv("gh_url", prev) })
	}
	if err := os.Unsetenv("gh_url"); err != nil {
		t.Fatalf("unsetting gh_url: %v", err)
	}

	got, err := notificationFromEnv()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.apiHost != "api.github.com" {
		t.Errorf("apiHost = %q, want api.github.com when gh_url is unset", got.apiHost)
	}
	if got.api != apiStatuses {
		t.Errorf("api = %q, want statuses when api is unset", got.api)
	}
}

func TestNotificationFromEnvNamesEveryMissingVariable(t *testing.T) {
	setNotificationEnv(t)
	t.Setenv("state", "")
	t.Setenv("git_sha", "")

	_, err := notificationFromEnv()

	if err == nil {
		t.Fatal("notificationFromEnv() = nil error, want the missing variables reported")
	}
	for _, name := range []string{"state", "git_sha"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name the missing variable %s", err, name)
		}
	}
}

func TestNotificationFromEnvRejectsNonNumericCheckRunID(t *testing.T) {
	setNotificationEnv(t)
	t.Setenv("api", "checks")
	t.Setenv("check_run_id", "not-a-number")

	_, err := notificationFromEnv()

	if err == nil {
		t.Fatal("notificationFromEnv() = nil error, want an error for a non-numeric check_run_id")
	}
	if !strings.Contains(err.Error(), "check_run_id") {
		t.Errorf("error %q does not name check_run_id", err)
	}
}

func TestNotificationFromEnvIgnoresCheckRunIDForStatuses(t *testing.T) {
	setNotificationEnv(t)
	t.Setenv("check_run_id", "not-a-number")

	got, err := notificationFromEnv()

	if err != nil {
		t.Fatalf("unexpected error: %v, want check_run_id ignored in statuses mode", err)
	}
	if got.checkRunID != 0 {
		t.Errorf("checkRunID = %d, want 0 in statuses mode", got.checkRunID)
	}
}

// clearCredentialEnv unsets every credential variable for the test.
func clearCredentialEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"app_id", "app_private_key", "app_private_key_file", "access_token", "tokenFile"} {
		t.Setenv(name, "")
	}
}

func TestCredentialsFromEnvPrefersAppOverAccessToken(t *testing.T) {
	clearCredentialEnv(t)
	want, pemBytes := testKeyPEM(t)
	t.Setenv("app_id", "12345")
	t.Setenv("app_private_key", base64.StdEncoding.EncodeToString(pemBytes))
	t.Setenv("access_token", "ghp_classicpat")

	got, err := credentialsFromEnv()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.useApp() || got.appID != "12345" || !got.appKey.Equal(want) {
		t.Errorf("credentialsFromEnv() = %+v, want the App credentials", got)
	}
	if got.accessToken != "" {
		t.Error("credentialsFromEnv() kept access_token alongside App credentials, want it ignored")
	}
}

func TestCredentialsFromEnvRefusesPartialAppConfig(t *testing.T) {
	clearCredentialEnv(t)
	t.Setenv("app_id", "12345")
	t.Setenv("access_token", "ghp_classicpat")

	_, err := credentialsFromEnv()

	if err == nil {
		t.Fatal("credentialsFromEnv() = nil error, want a refusal rather than a silent fall back to access_token")
	}
}

func TestCredentialsFromEnvReadsTokenFile(t *testing.T) {
	clearCredentialEnv(t)
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("token-from-file"), 0o600); err != nil {
		t.Fatalf("writing token file: %v", err)
	}
	t.Setenv("tokenFile", path)

	got, err := credentialsFromEnv()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.accessToken != "token-from-file" {
		t.Errorf("accessToken = %q, want the tokenFile contents", got.accessToken)
	}
}

func TestCredentialsFromEnvTrimsTokenFile(t *testing.T) {
	clearCredentialEnv(t)
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("token-from-file\n"), 0o600); err != nil {
		t.Fatalf("writing token file: %v", err)
	}
	t.Setenv("tokenFile", path)

	got, err := credentialsFromEnv()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.accessToken != "token-from-file" {
		t.Errorf("accessToken = %q, want the trailing newline trimmed so it stays out of the Authorization header", got.accessToken)
	}
}

func TestCredentialsFromEnvRejectsBlankTokenFile(t *testing.T) {
	clearCredentialEnv(t)
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
		t.Fatalf("writing token file: %v", err)
	}
	t.Setenv("tokenFile", path)

	if _, err := credentialsFromEnv(); err == nil {
		t.Fatal("credentialsFromEnv() = nil error, want an error for a tokenFile holding only whitespace")
	}
}

func TestCredentialsFromEnvPrefersAccessTokenOverTokenFile(t *testing.T) {
	clearCredentialEnv(t)
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("token-from-file"), 0o600); err != nil {
		t.Fatalf("writing token file: %v", err)
	}
	t.Setenv("access_token", "token-from-env")
	t.Setenv("tokenFile", path)

	got, err := credentialsFromEnv()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.accessToken != "token-from-env" {
		t.Errorf("accessToken = %q, want access_token to win over tokenFile", got.accessToken)
	}
}

func TestCredentialsFromEnvRequiresSomeCredential(t *testing.T) {
	clearCredentialEnv(t)

	_, err := credentialsFromEnv()

	if err == nil {
		t.Fatal("credentialsFromEnv() = nil error, want an error when nothing is configured")
	}
	if !strings.Contains(err.Error(), "access_token") {
		t.Errorf("error %q does not name access_token", err)
	}
}
