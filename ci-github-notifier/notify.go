package main

import (
	"fmt"

	"github.com/golang-jwt/jwt/v5"
	"github.com/imroc/req/v3"
)

// notify posts n to GitHub using creds. In checks mode it returns the
// check run's ID, which a later call needs to update it; in statuses
// mode the ID is always zero.
func notify(c *req.Client, n notification, creds credentials) (int64, error) {
	// Fail before authenticating: checks mode without App credentials
	// cannot work, and saying so beats a 403 from GitHub.
	if err := validateChecksMode(n.api, creds); err != nil {
		return 0, err
	}

	token, authPrefix, err := resolveToken(c, n, creds)
	if err != nil {
		return 0, err
	}
	auth := fmt.Sprintf("%s %s", authPrefix, token)

	if n.api == apiChecks {
		return notifyCheckRun(c, auth, n)
	}
	return 0, postStatus(c, auth, n)
}

// postStatus posts n as a commit status.
func postStatus(c *req.Client, auth string, n notification) error {
	values := map[string]string{
		"state":       n.state,
		"target_url":  n.targetURL,
		"description": n.description,
		"context":     n.context,
	}

	url := fmt.Sprintf("https://%s/repos/%s/%s/statuses/%s",
		n.apiHost, n.organisation, n.repo, n.sha)

	resp, err := c.R().
		SetHeader("Authorization", auth).
		SetBodyJsonMarshal(values).
		Post(url)
	if err != nil {
		return fmt.Errorf("posting commit status: %w", err)
	}

	fmt.Println("HTTP response from github:", resp.StatusCode)

	if !resp.IsSuccessState() {
		return fmt.Errorf("posting commit status: github returned %s: %s", resp.Status, resp)
	}
	return nil
}

// resolveToken returns the token to authenticate with and its
// Authorization scheme. App credentials are exchanged for an
// installation token scoped to n's repo and API.
func resolveToken(c *req.Client, n notification, creds credentials) (string, string, error) {
	if creds.useApp() {
		token, err := installationToken(c, creds, n.apiHost, n.organisation, n.repo, tokenPermissions(n.api))
		if err != nil {
			return "", "", err
		}
		return token, "Bearer", nil
	}

	if isJWT(creds.accessToken) {
		return creds.accessToken, "Bearer", nil
	}
	return creds.accessToken, "token", nil
}

// Checks if the given string is at least a structurally valid JWT. It does not verify signatures or claims.
func isJWT(tokenString string) bool {
	parser := jwt.NewParser()
	// give jwt.MapClaims as the claims type, but any valid claims type works
	_, _, err := parser.ParseUnverified(tokenString, jwt.MapClaims{})
	return err == nil
}
