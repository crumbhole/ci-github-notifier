// Command ci-github-notifier posts a commit status to the GitHub statuses
// API, so a CI system can report build state back to a pull request.
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/golang-jwt/jwt/v5"
	"github.com/imroc/req/v3"
)

func main() {
	fmt.Printf("Notifiying Github: %s:%s\n", getValidatedEnvVar("context"), getValidatedEnvVar("state"))

	// Create a client instead of using static methods
	client := req.C()

	apiHost := getURL("gh_url", "api.github.com")
	organisation := getValidatedEnvVar("organisation")
	appRepo := getValidatedEnvVar("app_repo")

	// Fail before authenticating: checks mode without App credentials
	// cannot work, and saying so beats a 403 from GitHub.
	if modeErr := validateChecksMode(); modeErr != nil {
		log.Fatal(modeErr)
	}

	token, authPrefix, authErr := resolveToken(client, apiHost, organisation, appRepo)
	if authErr != nil {
		log.Fatal(authErr)
	}

	auth := fmt.Sprintf("%s %s", authPrefix, token)

	if useChecks() {
		id, checkErr := notifyCheckRun(client, apiHost, auth, organisation, appRepo)
		if checkErr != nil {
			log.Fatal(checkErr)
		}
		fmt.Println("Check run:", id)
		if writeErr := writeCheckRunID(id); writeErr != nil {
			log.Fatal(writeErr)
		}
		return
	}

	values := map[string]string{
		"state":       getValidatedEnvVar("state"),
		"target_url":  getValidatedEnvVar("target_url"),
		"description": getValidatedEnvVar("description"),
		"context":     getValidatedEnvVar("context"),
	}

	// Build the URL
	url := fmt.Sprintf("https://%s/repos/%s/%s/statuses/%s",
		apiHost, organisation, appRepo, getValidatedEnvVar("git_sha"))

	resp, err := client.R().
		SetHeader("Authorization", auth).
		SetBodyJsonMarshal(values).
		Post(url)

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("HTTP response from github:", resp.StatusCode)

	if !resp.IsSuccessState() {
		log.Fatal(resp)
	}
}

func getValidatedEnvVar(e string) string {
	c := os.Getenv(e)
	if os.Getenv(e) == "" {
		fmt.Printf("Error: No environment variable called %s available. Exiting.\n", e)
		os.Exit(1)
	}
	return c
}

func getToken(f string, e string) string {
	if os.Getenv(e) == "" {
		// The token path comes from the tokenFile env var: operator-supplied
		// configuration, not attacker input, so the taint gosec reports here
		// (G304 file inclusion, G703 path traversal) is by design.
		// #nosec G304 G703
		data, err := os.ReadFile(f)
		if err != nil {
			fmt.Println("No tokenFile found. Falling back to Environment Variable")
		} else if err := os.Setenv(e, string(data)); err != nil {
			log.Fatalf("Could not set %s: %v", e, err)
		}
	}
	a := getValidatedEnvVar(e)
	return a
}

func getURL(e, fallback string) string {
	if value, ok := os.LookupEnv(e); ok {
		return value
	}
	return fallback
}

// Checks if the given string is at least a structurally valid JWT. It does not verify signatures or claims.
func isJWT(tokenString string) bool {
	parser := jwt.NewParser()
	// give jwt.MapClaims as the claims type, but any valid claims type works
	_, _, err := parser.ParseUnverified(tokenString, jwt.MapClaims{})
	return err == nil
}

// resolveToken picks the credential to authenticate with. GitHub App
// credentials win when configured; otherwise the access_token/tokenFile
// pair is used exactly as before.
func resolveToken(c *req.Client, apiHost, owner, repo string) (string, string, error) {
	useApp, err := appAuthConfigured()
	if err != nil {
		return "", "", err
	}

	if useApp {
		token, err := installationToken(c, apiHost, owner, repo, map[string]string{"statuses": "write"})
		if err != nil {
			return "", "", err
		}
		return token, "Bearer", nil
	}

	token := getToken(os.Getenv("tokenFile"), "access_token")
	if isJWT(token) {
		return token, "Bearer", nil
	}
	return token, "token", nil
}
