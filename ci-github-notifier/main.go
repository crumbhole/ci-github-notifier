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

	token := getToken(os.Getenv("tokenFile"), "access_token")
	authPrefix := "token"
	if isJWT(token) {
		authPrefix = "Bearer"
	}

	// Create a client instead of using static methods
	client := req.C()

	values := map[string]string{
		"state":       getValidatedEnvVar("state"),
		"target_url":  getValidatedEnvVar("target_url"),
		"description": getValidatedEnvVar("description"),
		"context":     getValidatedEnvVar("context"),
	}

	// Build the URL
	url := fmt.Sprintf("https://%s/repos/%s/%s/statuses/%s",
		getUrl("gh_url", "api.github.com"),
		getValidatedEnvVar("organisation"),
		getValidatedEnvVar("app_repo"),
		getValidatedEnvVar("git_sha"))

	resp, err := client.R().
		SetHeader("Authorization", fmt.Sprintf("%s %s", authPrefix, token)).
		SetBodyJsonMarshal(values).
		Post(url)

	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	fmt.Println("HTTP response from github:", resp.StatusCode)

	if ! resp.IsSuccessState() {
		log.Fatal(resp)
		os.Exit(1)
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
		data, err := os.ReadFile(f)
		if err != nil {
			fmt.Println("No tokenFile found. Falling back to Environment Variable")
		}
		os.Setenv(e, string(data))
	}
	a := getValidatedEnvVar(e)
	return a
}

func getUrl(e, fallback string) string {
	if value, ok := os.LookupEnv(e); ok {
		return value
	}
	return fallback
}

// Checks if the given string is at least a structurally valid JWT. It does not verify signatures or claims
func isJWT(tokenString string) bool {
	parser := jwt.NewParser()
	// give jwt.MapClaims as the claims type, but any valid claims type works
	_, _, err := parser.ParseUnverified(tokenString, jwt.MapClaims{})
	return err == nil
}
