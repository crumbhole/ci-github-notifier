package main

import (
	"encoding/json"
	"fmt"
	"github.com/imroc/req"
	"io/ioutil"
	"log"
	"os"
)

func main() {
	fmt.Printf("Notifiying Github: %s:%s\n", getValidatedEnvVar("context"), getValidatedEnvVar("state"))

	header := req.Header{
		"Authorization": fmt.Sprintf("token %s", getToken(os.Getenv("tokenFile"), "access_token")),
	}

	values := map[string]string{"state": getValidatedEnvVar("state"), "target_url": getValidatedEnvVar("target_url"), "description": getValidatedEnvVar("description"), "context": getValidatedEnvVar("context")}
	jsonData, err := json.Marshal(values)

	r, err := req.Post(fmt.Sprintf("https://%s/repos/%s/%s/statuses/%s", getUrl("gh_url", "api.github.com"), getValidatedEnvVar("organisation"), getValidatedEnvVar("app_repo"), getValidatedEnvVar("git_sha")), header, req.BodyJSON(jsonData))

	if err != nil {
		log.Fatal(err)
	}
	resp := r.Response()
	fmt.Println("HTTP response from github:", resp.StatusCode)
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
		data, err := ioutil.ReadFile(f)
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