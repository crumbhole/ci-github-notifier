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

// startCheckRuns starts a stub GitHub recording into rec, and returns a
// client for it and a pending check run notification aimed at it.
func startCheckRuns(t *testing.T, rec *checkRunRecorder) (*req.Client, notification) {
	t.Helper()
	srv := httptest.NewTLSServer(rec)
	t.Cleanup(srv.Close)

	n := notification{
		state:        "pending",
		targetURL:    "https://ci.example.com/run/1",
		description:  "Build running",
		context:      "ci/build",
		apiHost:      strings.TrimPrefix(srv.URL, "https://"),
		organisation: "crumbhole",
		repo:         "ci-github-notifier",
		sha:          "123abc",
		api:          apiChecks,
	}
	return req.C().EnableInsecureSkipVerify(), n
}

func TestCreateCheckRunPostsMappedFields(t *testing.T) {
	rec := &checkRunRecorder{}
	client, n := startCheckRuns(t, rec)

	id, err := postCheckRun(client, "token", n)

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
	client, n := startCheckRuns(t, rec)
	n.state = "success"
	n.description = "Build passed"
	n.checkRunID = 987654

	id, err := notifyCheckRun(client, "token", n)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 987654 {
		t.Errorf("id = %d, want the threaded check run id", id)
	}
	if rec.method != http.MethodPatch {
		t.Errorf("method = %s, want PATCH when a check run id is threaded", rec.method)
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
	client, n := startCheckRuns(t, rec)

	id, err := notifyCheckRun(client, "token", n)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 987654 {
		t.Errorf("id = %d, want the created check run id", id)
	}
	if rec.method != http.MethodPost {
		t.Errorf("method = %s, want POST when no check run id is threaded", rec.method)
	}
}

func TestWriteCheckRunIDWritesFileWhenRequested(t *testing.T) {
	path := filepath.Join(t.TempDir(), "id")

	if err := writeCheckRunID(path, 987654); err != nil {
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
	if err := writeCheckRunID("", 987654); err != nil {
		t.Errorf("unexpected error when no path is configured: %v", err)
	}
}

func TestChecksModeRequiresAppAuth(t *testing.T) {
	err := validateChecksMode(apiChecks, credentials{accessToken: "ghp_classicpat"})

	if err == nil {
		t.Fatal("validateChecksMode() = nil error, want a refusal: a PAT cannot create check runs")
	}
	if !strings.Contains(err.Error(), "GitHub App") {
		t.Errorf("error %q does not explain that check runs need a GitHub App", err)
	}
}

func TestChecksModeAcceptsAppAuth(t *testing.T) {
	key, _ := testKeyPEM(t)

	if err := validateChecksMode(apiChecks, credentials{appID: "12345", appKey: key}); err != nil {
		t.Errorf("unexpected error when App credentials are configured: %v", err)
	}
}

func TestStatusesModeDoesNotRequireAppAuth(t *testing.T) {
	if err := validateChecksMode(apiStatuses, credentials{accessToken: "ghp_classicpat"}); err != nil {
		t.Errorf("unexpected error in statuses mode: %v", err)
	}
}

func TestTokenPermissionsFollowTheMode(t *testing.T) {
	cases := []struct {
		mode string
		want string
	}{
		{apiStatuses, "statuses"},
		// Check runs need checks:write; asking for statuses:write here
		// would mint a token that cannot do the job.
		{apiChecks, "checks"},
	}

	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			perms := tokenPermissions(c.mode)

			if len(perms) != 1 {
				t.Fatalf("permissions = %v, want exactly one", perms)
			}
			if perms[c.want] != "write" {
				t.Errorf("permissions = %v, want %s:write", perms, c.want)
			}
		})
	}
}
