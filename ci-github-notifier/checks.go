package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/imroc/req/v3"
)

// checkRunState maps the tool's state onto the check run API's split of
// status and conclusion. error has no check run equivalent, so it
// reports as a failure, which is how the statuses UI already renders it.
func checkRunState(state string) (string, string, error) {
	switch state {
	case "pending":
		return "in_progress", "", nil
	case "success":
		return "completed", "success", nil
	case "failure", "error":
		return "completed", "failure", nil
	default:
		return "", "", fmt.Errorf("state %q is not one of pending, success, failure or error", state)
	}
}

// postCheckRun creates a check run and returns its ID, which a later
// invocation needs in order to update it.
func postCheckRun(c *req.Client, auth string, n notification) (int64, error) {
	body, err := checkRunBody(n)
	if err != nil {
		return 0, err
	}
	body["name"] = n.context
	body["head_sha"] = n.sha

	var created struct {
		ID int64 `json:"id"`
	}
	resp, err := c.R().
		SetHeader("Authorization", auth).
		SetBodyJsonMarshal(body).
		SetSuccessResult(&created).
		Post(fmt.Sprintf("https://%s/repos/%s/%s/check-runs", n.apiHost, n.organisation, n.repo))
	if err != nil {
		return 0, fmt.Errorf("creating check run: %w", err)
	}
	if !resp.IsSuccessState() {
		return 0, fmt.Errorf("creating check run: github returned %s", resp.Status)
	}
	return created.ID, nil
}

// checkRunBody builds the fields shared by creating and updating a
// check run. status and conclusion come from the tool's single state
// variable; conclusion is omitted while a run is still in progress,
// which GitHub requires.
func checkRunBody(n notification) (map[string]any, error) {
	status, conclusion, err := checkRunState(n.state)
	if err != nil {
		return nil, err
	}

	body := map[string]any{
		"status":      status,
		"details_url": n.targetURL,
		"output": map[string]any{
			"title":   n.context,
			"summary": n.description,
		},
	}
	if conclusion != "" {
		body["conclusion"] = conclusion
	}
	return body, nil
}

// notifyCheckRun creates a check run, or updates the one named by
// n.checkRunID. Check runs are objects with IDs rather than being keyed
// by commit and context, so a pipeline that reports pending and then a
// result has to carry the ID from the first call to the second.
func notifyCheckRun(c *req.Client, auth string, n notification) (int64, error) {
	if n.checkRunID == 0 {
		return postCheckRun(c, auth, n)
	}
	return n.checkRunID, patchCheckRun(c, auth, n)
}

// patchCheckRun updates an existing check run. name and head_sha are
// fixed at creation, so only the changing fields are sent.
func patchCheckRun(c *req.Client, auth string, n notification) error {
	id := n.checkRunID
	body, err := checkRunBody(n)
	if err != nil {
		return err
	}

	resp, err := c.R().
		SetHeader("Authorization", auth).
		SetBodyJsonMarshal(body).
		Patch(fmt.Sprintf("https://%s/repos/%s/%s/check-runs/%d", n.apiHost, n.organisation, n.repo, id))
	if err != nil {
		return fmt.Errorf("updating check run %d: %w", id, err)
	}
	if !resp.IsSuccessState() {
		return fmt.Errorf("updating check run %d: github returned %s", id, resp.Status)
	}
	return nil
}

// writeCheckRunID writes the check run's ID to path, which comes from
// check_run_id_file, so a later pipeline step can pass it back as
// check_run_id. An empty path writes nothing.
func writeCheckRunID(path string, id int64) error {
	if path == "" {
		return nil
	}

	// The path comes from check_run_id_file: operator-supplied
	// configuration, not attacker input. The id is not a secret, and a
	// pipeline step running as a different user often has to read it
	// back, hence the permissive mode.
	// #nosec G304 G703 G306
	if err := os.WriteFile(path, []byte(strconv.FormatInt(id, 10)), 0o644); err != nil {
		return fmt.Errorf("writing check_run_id_file: %w", err)
	}
	return nil
}

// validateChecksMode refuses checks mode without App credentials.
func validateChecksMode(mode string, creds credentials) error {
	if mode != apiChecks {
		return nil
	}
	if !creds.useApp() {
		return errors.New("api=checks requires GitHub App credentials: only a GitHub App can create check runs, so set app_id and a private key")
	}
	return nil
}

// tokenPermissions is the permission the installation token is minted
// with, which follows the API being used: a token scoped to statuses
// cannot write a check run, and vice versa.
func tokenPermissions(mode string) map[string]string {
	if mode == apiChecks {
		return map[string]string{"checks": "write"}
	}
	return map[string]string{"statuses": "write"}
}
