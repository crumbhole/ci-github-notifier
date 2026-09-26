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

const testAgentToken = "agent-token"

// newTestPlugin returns a plugin server posting to a stub GitHub served
// by github, authenticating with creds.
func newTestPlugin(t *testing.T, github http.Handler, creds credentials) *pluginServer {
	t.Helper()
	srv := httptest.NewTLSServer(github)
	t.Cleanup(srv.Close)

	tokenFile := filepath.Join(t.TempDir(), "token")
	// Written with a trailing newline, as a mounted secret may carry one.
	if err := os.WriteFile(tokenFile, []byte(testAgentToken+"\n"), 0o600); err != nil {
		t.Fatalf("writing agent token: %v", err)
	}

	return &pluginServer{
		client:    req.C().EnableInsecureSkipVerify(),
		creds:     creds,
		apiHost:   strings.TrimPrefix(srv.URL, "https://"),
		tokenFile: tokenFile,
	}
}

// callPlugin sends a template.execute call for a template whose plugin
// block is plugin, as the agent would.
func callPlugin(t *testing.T, s *pluginServer, plugin map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"workflow": map[string]any{"metadata": map[string]any{"name": "my-wf", "namespace": "argo"}},
		"template": map[string]any{"name": "notify", "plugin": plugin},
	})
	if err != nil {
		t.Fatalf("encoding request: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, executePath, strings.NewReader(string(body)))
	r.Header.Set("Authorization", "Bearer "+testAgentToken)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

// replyNode decodes the plugin's reply and returns its node.
func replyNode(t *testing.T, w *httptest.ResponseRecorder) *nodeResult {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", w.Code, w.Body)
	}
	var reply executeTemplateReply
	if err := json.NewDecoder(w.Body).Decode(&reply); err != nil {
		t.Fatalf("decoding reply: %v", err)
	}
	return reply.Node
}

func statusParams() map[string]any {
	return map[string]any{
		"state":        "success",
		"target_url":   "https://ci.example.com/run/1",
		"description":  "Build passed",
		"context":      "ci/build",
		"organisation": "crumbhole",
		"app_repo":     "ci-github-notifier",
		"git_sha":      "123abc",
	}
}

func TestPluginPostsStatus(t *testing.T) {
	rec := &statusRecorder{status: http.StatusCreated}
	s := newTestPlugin(t, rec, credentials{accessToken: "ghp_classicpat"})

	node := replyNode(t, callPlugin(t, s, map[string]any{pluginName: statusParams()}))

	if node == nil || node.Phase != phaseSucceeded {
		t.Fatalf("node = %+v, want Succeeded", node)
	}
	if rec.path != "/repos/crumbhole/ci-github-notifier/statuses/123abc" {
		t.Errorf("path = %s, want the commit's statuses", rec.path)
	}
	if rec.body["state"] != "success" {
		t.Errorf("state = %q, want success", rec.body["state"])
	}
	if node.Outputs != nil {
		t.Errorf("outputs = %+v, want none for a commit status", node.Outputs)
	}
}

func TestPluginIgnoresOtherPluginsTemplates(t *testing.T) {
	rec := &statusRecorder{status: http.StatusCreated}
	s := newTestPlugin(t, rec, credentials{accessToken: "ghp_classicpat"})

	w := callPlugin(t, s, map[string]any{"hello": map[string]any{}})

	// A nil node tells the agent to offer the template to the next
	// plugin. Any error here would stop it doing so.
	if node := replyNode(t, w); node != nil {
		t.Errorf("node = %+v, want none for a template belonging to another plugin", node)
	}
	if rec.path != "" {
		t.Errorf("GitHub was called at %s for another plugin's template", rec.path)
	}
}

func TestPluginCreatesCheckRunAndOutputsItsID(t *testing.T) {
	checks := &checkRunRecorder{}
	mux := http.NewServeMux()
	mux.Handle("/repos/crumbhole/ci-github-notifier/check-runs", checks)
	mux.Handle("/", &appAuthTestServer{})
	_, _, creds := startAppAuth(t, http.NotFoundHandler())
	s := newTestPlugin(t, mux, creds)
	params := statusParams()
	params["api"] = "checks"
	params["state"] = "pending"

	node := replyNode(t, callPlugin(t, s, map[string]any{pluginName: params}))

	if node == nil || node.Phase != phaseSucceeded {
		t.Fatalf("node = %+v, want Succeeded", node)
	}
	if checks.method != http.MethodPost {
		t.Errorf("method = %s, want POST creating the check run", checks.method)
	}
	want := []nodeParameter{{Name: "check_run_id", Value: "987654"}}
	if node.Outputs == nil || len(node.Outputs.Parameters) != 1 || node.Outputs.Parameters[0] != want[0] {
		t.Errorf("outputs = %+v, want check_run_id for a later step to update", node.Outputs)
	}
}

func TestPluginUpdatesCheckRunFromNumericID(t *testing.T) {
	checks := &checkRunRecorder{}
	mux := http.NewServeMux()
	mux.Handle("/repos/crumbhole/ci-github-notifier/check-runs/987654", checks)
	mux.Handle("/", &appAuthTestServer{})
	_, _, creds := startAppAuth(t, http.NotFoundHandler())
	s := newTestPlugin(t, mux, creds)
	params := statusParams()
	params["api"] = "checks"
	// An unquoted YAML value arrives as a JSON number.
	params["check_run_id"] = 987654

	node := replyNode(t, callPlugin(t, s, map[string]any{pluginName: params}))

	if node == nil || node.Phase != phaseSucceeded {
		t.Fatalf("node = %+v, want Succeeded", node)
	}
	if checks.method != http.MethodPatch {
		t.Errorf("method = %s, want PATCH updating the threaded check run", checks.method)
	}
}

func TestPluginFailsNodeOnBadParameters(t *testing.T) {
	cases := map[string]struct {
		mutate func(map[string]any)
		want   []string
	}{
		"missing": {
			mutate: func(p map[string]any) { delete(p, "state"); delete(p, "git_sha") },
			want:   []string{"plugin parameter called state", "plugin parameter called git_sha"},
		},
		"typo": {
			mutate: func(p map[string]any) { p["gitsha"] = "123abc" },
			want:   []string{"unknown plugin parameter gitsha"},
		},
		// gh_url and the credentials are sidecar configuration; a
		// workflow must not be able to redirect the credentials.
		"gh_url": {
			mutate: func(p map[string]any) { p["gh_url"] = "attacker.example.com" },
			want:   []string{"unknown plugin parameter gh_url"},
		},
		"access_token": {
			mutate: func(p map[string]any) { p["access_token"] = "ghp_other" },
			want:   []string{"unknown plugin parameter access_token"},
		},
		"non-string": {
			mutate: func(p map[string]any) { p["state"] = []string{"success"} },
			want:   []string{"plugin parameter state must be a string"},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rec := &statusRecorder{status: http.StatusCreated}
			s := newTestPlugin(t, rec, credentials{accessToken: "ghp_classicpat"})
			params := statusParams()
			c.mutate(params)

			node := replyNode(t, callPlugin(t, s, map[string]any{pluginName: params}))

			if node == nil || node.Phase != phaseFailed {
				t.Fatalf("node = %+v, want Failed", node)
			}
			for _, want := range c.want {
				if !strings.Contains(node.Message, want) {
					t.Errorf("message %q does not contain %q", node.Message, want)
				}
			}
			if rec.path != "" {
				t.Errorf("GitHub was called at %s despite bad parameters", rec.path)
			}
		})
	}
}

func TestPluginFailsNodeWhenGitHubRejects(t *testing.T) {
	rec := &statusRecorder{status: http.StatusUnprocessableEntity}
	s := newTestPlugin(t, rec, credentials{accessToken: "ghp_classicpat"})

	node := replyNode(t, callPlugin(t, s, map[string]any{pluginName: statusParams()}))

	if node == nil || node.Phase != phaseFailed {
		t.Fatalf("node = %+v, want Failed", node)
	}
	if !strings.Contains(node.Message, "422") {
		t.Errorf("message %q does not carry GitHub's status", node.Message)
	}
}

func TestPluginRefusesCallsWithoutTheAgentToken(t *testing.T) {
	for name, auth := range map[string]string{
		"missing": "",
		"wrong":   "Bearer not-the-agent",
		"bare":    testAgentToken,
	} {
		t.Run(name, func(t *testing.T) {
			rec := &statusRecorder{status: http.StatusCreated}
			s := newTestPlugin(t, rec, credentials{accessToken: "ghp_classicpat"})
			r := httptest.NewRequest(http.MethodPost, executePath, strings.NewReader(`{}`))
			if auth != "" {
				r.Header.Set("Authorization", auth)
			}
			w := httptest.NewRecorder()

			s.ServeHTTP(w, r)

			if w.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403", w.Code)
			}
		})
	}
}

func TestPluginAnswersNotFoundForOtherMethods(t *testing.T) {
	s := newTestPlugin(t, http.NotFoundHandler(), credentials{accessToken: "ghp_classicpat"})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/something.else", strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer "+testAgentToken)
	w := httptest.NewRecorder()

	s.ServeHTTP(w, r)

	// The agent stops calling a method that answers 404.
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
