package main

import (
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// The accepted values of the api variable.
const (
	apiStatuses = "statuses"
	apiChecks   = "checks"
)

// notification is what to report and where. It is kept apart from
// credentials so that a front end other than environment variables can
// supply the per-call fields while the credentials stay configured once.
type notification struct {
	state       string
	targetURL   string
	description string
	context     string

	apiHost      string
	organisation string
	repo         string
	sha          string

	// api is apiStatuses or apiChecks.
	api string
	// checkRunIDFile is where to write a created check run's ID, if set.
	checkRunIDFile string
	// checkRunID is the check run to update; zero creates a new one.
	checkRunID int64
}

// credentials is how to authenticate to GitHub: either a GitHub App or
// an access token, never both.
type credentials struct {
	appID       string
	appKey      *rsa.PrivateKey
	accessToken string
}

// useApp reports whether the credentials are for a GitHub App.
func (c credentials) useApp() bool {
	return c.appID != ""
}

// notificationFromEnv reads a notification from the environment. Every
// missing required variable is reported at once, so a misconfigured
// step can be fixed in one go.
func notificationFromEnv() (notification, error) {
	n := notification{
		state:          os.Getenv("state"),
		targetURL:      os.Getenv("target_url"),
		description:    os.Getenv("description"),
		context:        os.Getenv("context"),
		apiHost:        envOr("gh_url", "api.github.com"),
		organisation:   os.Getenv("organisation"),
		repo:           os.Getenv("app_repo"),
		sha:            os.Getenv("git_sha"),
		checkRunIDFile: os.Getenv("check_run_id_file"),
	}

	var errs []error
	for _, required := range []struct{ name, value string }{
		{"state", n.state},
		{"target_url", n.targetURL},
		{"description", n.description},
		{"context", n.context},
		{"organisation", n.organisation},
		{"app_repo", n.repo},
		{"git_sha", n.sha},
	} {
		if required.value == "" {
			errs = append(errs, fmt.Errorf("no environment variable called %s available", required.name))
		}
	}

	api, err := apiMode(os.Getenv("api"))
	if err != nil {
		errs = append(errs, err)
	}
	n.api = api

	// check_run_id only means something to the checks API; a stray one
	// in statuses mode is ignored, as it always has been.
	if threaded := os.Getenv("check_run_id"); threaded != "" && api == apiChecks {
		id, err := strconv.ParseInt(threaded, 10, 64)
		if err != nil {
			errs = append(errs, fmt.Errorf("check_run_id %q is not a number: %w", threaded, err))
		}
		n.checkRunID = id
	}

	return n, errors.Join(errs...)
}

// apiMode returns which GitHub API to post to. Anything that is not
// exactly statuses or checks is an error rather than a silent fall back:
// api=Checks, or a typo like api=check, would otherwise post a commit
// status and exit 0, which is indistinguishable from working.
func apiMode(mode string) (string, error) {
	switch mode {
	case "":
		return apiStatuses, nil
	case apiStatuses, apiChecks:
		return mode, nil
	default:
		return "", fmt.Errorf("api %q is not one of %q or %q", mode, apiStatuses, apiChecks)
	}
}

// credentialsFromEnv reads the GitHub credentials from the environment.
// GitHub App credentials win when configured; otherwise the
// access_token/tokenFile pair is used.
func credentialsFromEnv() (credentials, error) {
	useApp, err := appAuthConfigured()
	if err != nil {
		return credentials{}, err
	}

	if useApp {
		key, keyErr := appPrivateKey()
		if keyErr != nil {
			return credentials{}, keyErr
		}
		return credentials{appID: os.Getenv("app_id"), appKey: key}, nil
	}

	token, tokenErr := accessToken()
	if tokenErr != nil {
		return credentials{}, tokenErr
	}
	return credentials{accessToken: token}, nil
}

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

// accessToken returns access_token, or failing that the contents of the
// file named by tokenFile.
func accessToken() (string, error) {
	if token := os.Getenv("access_token"); token != "" {
		return token, nil
	}

	path := os.Getenv("tokenFile")
	if path == "" {
		return "", errors.New("no environment variable called access_token available")
	}

	// The token path comes from the tokenFile env var: operator-supplied
	// configuration, not attacker input, so the taint gosec reports here
	// (G304 file inclusion, G703 path traversal) is by design.
	// #nosec G304 G703
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("access_token is unset and reading tokenFile failed: %w", err)
	}
	// Files written by editors, echo or secret injectors usually end in
	// a newline, which would otherwise end up in the Authorization header.
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("access_token is unset and tokenFile %s is empty", path)
	}
	return token, nil
}

func envOr(e, fallback string) string {
	if value, ok := os.LookupEnv(e); ok {
		return value
	}
	return fallback
}
