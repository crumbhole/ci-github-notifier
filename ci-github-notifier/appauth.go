package main

import (
	"crypto/rsa"
	"fmt"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/imroc/req/v3"
)

// appJWT builds the short-lived JWT that authenticates as the App
// itself, which is the credential GitHub accepts for minting an
// installation token.
func appJWT(appID string, key *rsa.PrivateKey, now time.Time) (string, error) {
	// iat is backdated a minute because GitHub rejects tokens issued in
	// its own future, and CI clocks drift. exp is kept inside GitHub's
	// ten minute ceiling with a minute to spare.
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Issuer:    appID,
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)),
		ExpiresAt: jwt.NewNumericDate(now.Add(9 * time.Minute)),
	})

	signed, err := token.SignedString(key)
	if err != nil {
		return "", fmt.Errorf("signing GitHub App JWT: %w", err)
	}
	return signed, nil
}

// installationToken mints a short-lived installation access token for
// owner/repo using the GitHub App credentials in creds.
func installationToken(c *req.Client, creds credentials, apiHost, owner, repo string, permissions map[string]string) (string, error) {
	signed, err := appJWT(creds.appID, creds.appKey, time.Now())
	if err != nil {
		return "", err
	}

	id, err := installationID(c, creds.appID, apiHost, owner, repo, signed)
	if err != nil {
		return "", err
	}

	var minted struct {
		Token string `json:"token"`
	}
	// Without a body GitHub mints a token good for every repository the
	// App is installed on and every permission it holds. Narrow it to the
	// one repo and the one permission this run actually needs.
	scope := map[string]any{
		"repositories": []string{repo},
		"permissions":  permissions,
	}

	resp, err := c.R().
		SetHeader("Authorization", "Bearer "+signed).
		SetBodyJsonMarshal(scope).
		SetSuccessResult(&minted).
		Post(fmt.Sprintf("https://%s/app/installations/%d/access_tokens", apiHost, id))
	if err != nil {
		return "", fmt.Errorf("minting installation token: %w", err)
	}
	if !resp.IsSuccessState() {
		return "", fmt.Errorf("minting installation token: github returned %s", resp.Status)
	}
	return minted.Token, nil
}

// installationID finds the App's installation on owner/repo. Looking it
// up keeps installation_id out of the configuration: the App can only
// act on repos it is installed on, so the ID is derivable from the repo
// the caller is already naming.
func installationID(c *req.Client, appID, apiHost, owner, repo, signed string) (int64, error) {
	var installation struct {
		ID int64 `json:"id"`
	}
	resp, err := c.R().
		SetHeader("Authorization", "Bearer "+signed).
		SetSuccessResult(&installation).
		Get(fmt.Sprintf("https://%s/repos/%s/%s/installation", apiHost, owner, repo))
	if err != nil {
		return 0, fmt.Errorf("looking up GitHub App installation: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return 0, fmt.Errorf("GitHub App %s is not installed on %s/%s", appID, owner, repo)
	}
	if !resp.IsSuccessState() {
		return 0, fmt.Errorf("looking up GitHub App installation: github returned %s", resp.Status)
	}
	return installation.ID, nil
}
