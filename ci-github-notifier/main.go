package main

import (
	"encoding/json"
	"github.com/imroc/req"
    "flag"
    "fmt"
    "log"
    "net/http"
    "os"
	"strings"
)

func main() {
	var webServerModeFlag = flag.Bool("webServerMode", false, "Run in web server (daemon service) mode")
	flag.Parse()

    // Flag can be overridden by environment variable
    webServerModeEnv := strings.ToLower(os.Getenv("WEBSERVER_MODE")) == "true"

    // Determine web server mode by combining the flag and the environment variable
    webServerMode := *webServerModeFlag || webServerModeEnv

    if webServerMode {
		fmt.Println("Running in web server mode")
        startWebServer()
    } else {
		fmt.Println("Running in standalone mode")
        notifyGitHub(nil, nil)
    }
}

func startWebServer() {
    http.HandleFunc("/notify", func(w http.ResponseWriter, r *http.Request) {
		notifyGitHub(w, r)
	})
    fmt.Println("Server is listening on :8080")
    log.Fatal(http.ListenAndServe(":8080", nil))
}

func notifyGitHub(w http.ResponseWriter, r *http.Request) {
    // Helper function to get parameter value: first tries URL query, then falls back to environment variable.
    getParamValue := func(paramName, envVarName string) string {
        if r != nil { // Check if http.Request is available
            values := r.URL.Query()
            if value, found := values[paramName]; found && len(value) > 0 {
                return value[0]
            }
        }
        return getValidatedEnvVar(envVarName)
    }

    // Parameters
    state := getParamValue("state", "state")
    targetURL := getParamValue("target_url", "target_url")
    description := getParamValue("description", "description")
    context := getParamValue("context", "context")
    organisation := getParamValue("organisation", "organisation")
    appRepo := getParamValue("app_repo", "app_repo")
    gitSha := getParamValue("git_sha", "git_sha")
    ghURL := getParamValue("gh_url", "gh_url")

    // Notification logic
    fmt.Printf("Notifying Github: %s:%s\n", context, state)

    header := req.Header{
        "Authorization": fmt.Sprintf("token %s", getToken(os.Getenv("tokenFile"), "access_token")),
    }

    values := map[string]string{"state": state, "target_url": targetURL, "description": description, "context": context}
    jsonData, err := json.Marshal(values)

    resp, err := req.Post(fmt.Sprintf("https://%s/repos/%s/%s/statuses/%s", getUrl(ghURL, "api.github.com"), organisation, appRepo, gitSha), header, req.BodyJSON(jsonData))
    if err != nil {
        if w != nil { // Check if http.ResponseWriter is available
            http.Error(w, err.Error(), http.StatusInternalServerError)
        } else {
            fmt.Println("Error notifying GitHub:", err)
        }
        return
    }

    httpResponse := resp.Response()
    if w != nil {
        fmt.Fprintf(w, "HTTP response from github: %d", httpResponse.StatusCode)
    } else {
        fmt.Printf("HTTP response from github: %d\n", httpResponse.StatusCode)
    }
}

func getValidatedEnvVar(e string) string {
    c := os.Getenv(e)
    if os.Getenv(e) == "" {
        log.Fatalf("Error: No environment variable called %s available. Exiting.\n", e)
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
