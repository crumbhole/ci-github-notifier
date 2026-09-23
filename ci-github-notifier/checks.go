package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/imroc/req/v3"
)

// The accepted values of the api variable.
const (
	apiStatuses = "statuses"
	apiChecks   = "checks"
)

// apiMode returns which GitHub API to post to. Anything that is not
// exactly statuses or checks is an error rather than a silent fall back:
// api=Checks, or a typo like api=check, would otherwise post a commit
// status and exit 0, which is indistinguishable from working.
func apiMode() (string, error) {
	switch mode := os.Getenv("api"); mode {
	case "":
		return apiStatuses, nil
	case apiStatuses, apiChecks:
		return mode, nil
	default:
		return "", fmt.Errorf("api %q is not one of %q or %q", mode, apiStatuses, apiChecks)
	}
}

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
func postCheckRun(c *req.Client, apiHost, auth, owner, repo string) (int64, error) {
	body, err := checkRunBody()
	if err != nil {
		return 0, err
	}
	body["name"] = getValidatedEnvVar("context")
	body["head_sha"] = getValidatedEnvVar("git_sha")

	var created struct {
		ID int64 `json:"id"`
	}
	resp, err := c.R().
		SetHeader("Authorization", auth).
		SetBodyJsonMarshal(body).
		SetSuccessResult(&created).
		Post(fmt.Sprintf("https://%s/repos/%s/%s/check-runs", apiHost, owner, repo))
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
func checkRunBody() (map[string]any, error) {
	status, conclusion, err := checkRunState(getValidatedEnvVar("state"))
	if err != nil {
		return nil, err
	}

	body := map[string]any{
		"status":      status,
		"details_url": getValidatedEnvVar("target_url"),
		"output": map[string]any{
			"title":   getValidatedEnvVar("context"),
			"summary": getValidatedEnvVar("description"),
		},
	}
	if conclusion != "" {
		body["conclusion"] = conclusion
	}
	return body, nil
}

// notifyCheckRun creates a check run, or updates the one named by
// check_run_id. Check runs are objects with IDs rather than being keyed
// by commit and context, so a pipeline that reports pending and then a
// result has to carry the ID from the first call to the second.
func notifyCheckRun(c *req.Client, apiHost, auth, owner, repo string) (int64, error) {
	threaded := os.Getenv("check_run_id")
	if threaded == "" {
		return postCheckRun(c, apiHost, auth, owner, repo)
	}

	id, err := strconv.ParseInt(threaded, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("check_run_id %q is not a number: %w", threaded, err)
	}
	return id, patchCheckRun(c, apiHost, auth, owner, repo, id)
}

// patchCheckRun updates an existing check run. name and head_sha are
// fixed at creation, so only the changing fields are sent.
func patchCheckRun(c *req.Client, apiHost, auth, owner, repo string, id int64) error {
	body, err := checkRunBody()
	if err != nil {
		return err
	}

	resp, err := c.R().
		SetHeader("Authorization", auth).
		SetBodyJsonMarshal(body).
		Patch(fmt.Sprintf("https://%s/repos/%s/%s/check-runs/%d", apiHost, owner, repo, id))
	if err != nil {
		return fmt.Errorf("updating check run %d: %w", id, err)
	}
	if !resp.IsSuccessState() {
		return fmt.Errorf("updating check run %d: github returned %s", id, resp.Status)
	}
	return nil
}

// writeCheckRunID writes the check run's ID to check_run_id_file so a
// later pipeline step can pass it back as check_run_id.
func writeCheckRunID(id int64) error {
	path := os.Getenv("check_run_id_file")
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
func validateChecksMode(mode string) error {
	if mode != apiChecks {
		return nil
	}

	configured, err := appAuthConfigured()
	if err != nil {
		return err
	}
	if !configured {
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
