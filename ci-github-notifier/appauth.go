package main

import (
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/imroc/req/v3"
)

// appAuthConfigured reports whether GitHub App credentials are present.
// Partial configuration is an error rather than a silent fall back to
// access_token: it almost always means a half-finished migration, and
// quietly using the old credential hides that.
func appAuthConfigured() (bool, error) {
	id := os.Getenv("app_id")
	hasKey := os.Getenv("app_private_key") != "" || os.Getenv("app_private_key_file") != ""

	switch {
	case id == "" && !hasKey:
		return false, nil
	case id == "":
		return false, errors.New("app_private_key or app_private_key_file is set but app_id is not")
	case !hasKey:
		return false, errors.New("app_id is set but neither app_private_key nor app_private_key_file is")
	default:
		return true, nil
	}
}

// appPrivateKey loads the GitHub App private key from app_private_key
// (base64-encoded PEM) or, failing that, app_private_key_file (raw PEM).
func appPrivateKey() (*rsa.PrivateKey, error) {
	pemBytes, err := appPrivateKeyPEM()
	if err != nil {
		return nil, err
	}

	key, err := jwt.ParseRSAPrivateKeyFromPEM(pemBytes)
	if err != nil {
		return nil, fmt.Errorf("parsing GitHub App private key: %w", err)
	}
	return key, nil
}

// appPrivateKeyPEM returns the raw PEM bytes of the App private key.
// app_private_key holds it base64-encoded, which keeps a multi-line PEM
// out of environment variables; app_private_key_file is the raw file.
func appPrivateKeyPEM() ([]byte, error) {
	if encoded := os.Getenv("app_private_key"); encoded != "" {
		pemBytes, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("decoding app_private_key as base64: %w", err)
		}
		return pemBytes, nil
	}

	path := os.Getenv("app_private_key_file")
	pemBytes, err := os.ReadFile(path) // #nosec G304 G703 -- operator-supplied configuration.
	if err != nil {
		return nil, fmt.Errorf("reading app_private_key_file: %w", err)
	}
	return pemBytes, nil
}

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
// owner/repo using the configured GitHub App credentials.
func installationToken(c *req.Client, apiHost, owner, repo string, permissions map[string]string) (string, error) {
	key, err := appPrivateKey()
	if err != nil {
		return "", err
	}

	signed, err := appJWT(os.Getenv("app_id"), key, time.Now())
	if err != nil {
		return "", err
	}

	id, err := installationID(c, apiHost, owner, repo, signed)
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
func installationID(c *req.Client, apiHost, owner, repo, signed string) (int64, error) {
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
		return 0, fmt.Errorf("GitHub App %s is not installed on %s/%s", os.Getenv("app_id"), owner, repo)
	}
	if !resp.IsSuccessState() {
		return 0, fmt.Errorf("looking up GitHub App installation: github returned %s", resp.Status)
	}
	return installation.ID, nil
}
