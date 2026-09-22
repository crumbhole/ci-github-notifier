package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imroc/req/v3"
)

func TestCheckRunStateMapping(t *testing.T) {
	cases := []struct {
		state          string
		wantStatus     string
		wantConclusion string
	}{
		{"pending", "in_progress", ""},
		{"success", "completed", "success"},
		{"failure", "completed", "failure"},
		// error has no check run equivalent; it reports as a failure the
		// same way the statuses UI already renders it.
		{"error", "completed", "failure"},
	}

	for _, c := range cases {
		t.Run(c.state, func(t *testing.T) {
			status, conclusion, err := checkRunState(c.state)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if status != c.wantStatus {
				t.Errorf("status = %q, want %q", status, c.wantStatus)
			}
			if conclusion != c.wantConclusion {
				t.Errorf("conclusion = %q, want %q", conclusion, c.wantConclusion)
			}
		})
	}
}

func TestCheckRunStateRejectsUnknownState(t *testing.T) {
	_, _, err := checkRunState("banana")

	if err == nil {
		t.Fatal("checkRunState() = nil error, want an error for a state GitHub does not accept")
	}
}

// checkRunRecorder captures the check run request GitHub would receive.
type checkRunRecorder struct {
	body   map[string]any
	method string
	path   string
}

func (c *checkRunRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.method = r.Method
	c.path = r.URL.Path
	if err := json.NewDecoder(r.Body).Decode(&c.body); err != nil {
		c.body = map[string]any{"decode error": err.Error()}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(`{"id": 987654}`))
}

func checkRunEnv(t *testing.T) {
	t.Helper()
	t.Setenv("state", "pending")
	t.Setenv("target_url", "https://ci.example.com/run/1")
	t.Setenv("description", "Build running")
	t.Setenv("context", "ci/build")
	t.Setenv("git_sha", "123abc")
	t.Setenv("check_run_id", "")
	t.Setenv("check_run_id_file", "")
}

func TestCreateCheckRunPostsMappedFields(t *testing.T) {
	rec := &checkRunRecorder{}
	srv := httptest.NewTLSServer(rec)
	t.Cleanup(srv.Close)
	checkRunEnv(t)
	client := req.C().EnableInsecureSkipVerify()
	host := strings.TrimPrefix(srv.URL, "https://")

	id, err := postCheckRun(client, host, "token", "crumbhole", "ci-github-notifier")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 987654 {
		t.Errorf("id = %d, want the id from the response", id)
	}
	if rec.method != http.MethodPost {
		t.Errorf("method = %s, want POST when creating a check run", rec.method)
	}
	if rec.path != "/repos/crumbhole/ci-github-notifier/check-runs" {
		t.Errorf("path = %s, want the check-runs collection", rec.path)
	}
	want := map[string]any{
		"name":        "ci/build",
		"head_sha":    "123abc",
		"status":      "in_progress",
		"details_url": "https://ci.example.com/run/1",
	}
	for k, v := range want {
		if rec.body[k] != v {
			t.Errorf("body[%q] = %v, want %v", k, rec.body[k], v)
		}
	}
	if _, ok := rec.body["conclusion"]; ok {
		t.Error("body carries a conclusion, which GitHub rejects for an in_progress check run")
	}
	output, ok := rec.body["output"].(map[string]any)
	if !ok {
		t.Fatalf("body[output] = %v, want an object carrying the description", rec.body["output"])
	}
	if output["summary"] != "Build running" {
		t.Errorf("output.summary = %v, want the description", output["summary"])
	}
}

func TestUpdateCheckRunPatchesByThreadedID(t *testing.T) {
	rec := &checkRunRecorder{}
	srv := httptest.NewTLSServer(rec)
	t.Cleanup(srv.Close)
	checkRunEnv(t)
	t.Setenv("state", "success")
	t.Setenv("description", "Build passed")
	t.Setenv("check_run_id", "987654")
	client := req.C().EnableInsecureSkipVerify()
	host := strings.TrimPrefix(srv.URL, "https://")

	id, err := notifyCheckRun(client, host, "token", "crumbhole", "ci-github-notifier")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 987654 {
		t.Errorf("id = %d, want the threaded check run id", id)
	}
	if rec.method != http.MethodPatch {
		t.Errorf("method = %s, want PATCH when check_run_id is set", rec.method)
	}
	if rec.path != "/repos/crumbhole/ci-github-notifier/check-runs/987654" {
		t.Errorf("path = %s, want the individual check run", rec.path)
	}
	if rec.body["status"] != "completed" {
		t.Errorf("status = %v, want completed", rec.body["status"])
	}
	if rec.body["conclusion"] != "success" {
		t.Errorf("conclusion = %v, want success", rec.body["conclusion"])
	}
	if _, ok := rec.body["head_sha"]; ok {
		t.Error("update body carries head_sha, which belongs only on create")
	}
}

func TestNotifyCheckRunCreatesWhenNoIDThreaded(t *testing.T) {
	rec := &checkRunRecorder{}
	srv := httptest.NewTLSServer(rec)
	t.Cleanup(srv.Close)
	checkRunEnv(t)
	client := req.C().EnableInsecureSkipVerify()
	host := strings.TrimPrefix(srv.URL, "https://")

	id, err := notifyCheckRun(client, host, "token", "crumbhole", "ci-github-notifier")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 987654 {
		t.Errorf("id = %d, want the created check run id", id)
	}
	if rec.method != http.MethodPost {
		t.Errorf("method = %s, want POST when no check_run_id is set", rec.method)
	}
}

func TestNotifyCheckRunRejectsNonNumericID(t *testing.T) {
	checkRunEnv(t)
	t.Setenv("check_run_id", "not-a-number")

	_, err := notifyCheckRun(req.C(), "api.github.com", "token", "crumbhole", "ci-github-notifier")

	if err == nil {
		t.Fatal("notifyCheckRun() = nil error, want an error for a non-numeric check_run_id")
	}
	if !strings.Contains(err.Error(), "check_run_id") {
		t.Errorf("error %q does not name check_run_id", err)
	}
}

func TestWriteCheckRunIDWritesFileWhenRequested(t *testing.T) {
	path := filepath.Join(t.TempDir(), "id")
	t.Setenv("check_run_id_file", path)

	if err := writeCheckRunID(987654); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(path) // #nosec G304 -- path is from t.TempDir()
	if err != nil {
		t.Fatalf("reading id file: %v", err)
	}
	if string(got) != "987654" {
		t.Errorf("file contains %q, want the bare id for a pipeline to read back", got)
	}
}

func TestWriteCheckRunIDIsANoopWithoutAPath(t *testing.T) {
	t.Setenv("check_run_id_file", "")

	if err := writeCheckRunID(987654); err != nil {
		t.Errorf("unexpected error when no path is configured: %v", err)
	}
}

func TestChecksModeRequiresAppAuth(t *testing.T) {
	t.Setenv("api", "checks")
	t.Setenv("app_id", "")
	t.Setenv("app_private_key", "")
	t.Setenv("app_private_key_file", "")

	err := validateChecksMode()

	if err == nil {
		t.Fatal("validateChecksMode() = nil error, want a refusal: a PAT cannot create check runs")
	}
	if !strings.Contains(err.Error(), "GitHub App") {
		t.Errorf("error %q does not explain that check runs need a GitHub App", err)
	}
}

func TestChecksModeAcceptsAppAuth(t *testing.T) {
	t.Setenv("api", "checks")
	t.Setenv("app_id", "12345")
	t.Setenv("app_private_key", "cGVt")
	t.Setenv("app_private_key_file", "")

	if err := validateChecksMode(); err != nil {
		t.Errorf("unexpected error when App credentials are configured: %v", err)
	}
}

func TestStatusesModeDoesNotRequireAppAuth(t *testing.T) {
	t.Setenv("api", "statuses")
	t.Setenv("app_id", "")
	t.Setenv("app_private_key", "")
	t.Setenv("app_private_key_file", "")

	if err := validateChecksMode(); err != nil {
		t.Errorf("unexpected error in statuses mode: %v", err)
	}
}

func TestUseChecksDefaultsToStatuses(t *testing.T) {
	t.Setenv("api", "")

	if useChecks() {
		t.Error("useChecks() = true with api unset, want statuses to stay the default")
	}
}

func TestUseChecksWhenRequested(t *testing.T) {
	t.Setenv("api", "checks")

	if !useChecks() {
		t.Error("useChecks() = false with api=checks")
	}
}
