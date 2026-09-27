package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"maps"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/imroc/req/v3"
)

// Running as an Argo Workflows executor plugin: a sidecar in each
// workflow's agent pod, which the agent calls over localhost for every
// template carrying a plugin: block.
// See https://argo-workflows.readthedocs.io/en/latest/executor_plugins/.
const (
	// pluginName is the key a template uses under plugin: to call us.
	pluginName = "ci-github-notifier"

	// executePath is the one method the agent calls on a plugin.
	executePath = "/api/v1/template.execute"

	// defaultTokenFile is where the agent mounts the token it sends as
	// a bearer token, so a plugin can check the caller is its agent.
	defaultTokenFile = "/var/run/argo/token" // #nosec G101 -- a path, not a credential.

	// githubRequestTimeout bounds each GitHub call. A notification makes
	// at most three (installation lookup, token mint, post), which has
	// to finish inside the agent's 30s timeout: the agent retries a
	// timed-out call, and a retried create would post a second check run.
	githubRequestTimeout = 8 * time.Second

	// maxRequestBytes caps the request body. It carries one template with
	// its parameters substituted, which Kubernetes' object size limit
	// (about 1.5MiB in etcd) keeps well under this. The body is only read
	// once the caller has shown the agent's token.
	maxRequestBytes = 8 << 20
)

// The phases a node can finish in.
const (
	phaseSucceeded = "Succeeded"
	phaseFailed    = "Failed"
)

// executeTemplateArgs is the part of the agent's request we use.
type executeTemplateArgs struct {
	Workflow struct {
		Metadata struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
	} `json:"workflow"`
	Template struct {
		Plugin map[string]json.RawMessage `json:"plugin"`
		Name   string                     `json:"name"`
	} `json:"template"`
}

// executeTemplateReply is the agent's expected response. A nil node
// tells the agent the template belongs to some other plugin.
type executeTemplateReply struct {
	Node *nodeResult `json:"node,omitempty"`
}

type nodeResult struct {
	Outputs *nodeOutputs `json:"outputs,omitempty"`
	Phase   string       `json:"phase"`
	Message string       `json:"message"`
}

type nodeOutputs struct {
	Parameters []nodeParameter `json:"parameters"`
}

type nodeParameter struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// pluginServer answers the agent's template.execute calls.
type pluginServer struct {
	client    *req.Client
	creds     credentials
	apiHost   string
	tokenFile string
}

// runPlugin serves the executor plugin API until SIGTERM or SIGINT.
func runPlugin(args []string) error {
	flags := flag.NewFlagSet("plugin", flag.ContinueOnError)
	address := flags.String("address", "127.0.0.1:7384", "address to listen on; the agent calls plugins on localhost")
	tokenFile := flags.String("token-file", defaultTokenFile, "file holding the token the agent authenticates with")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return fmt.Errorf("parsing plugin flags: %w", err)
	}

	// Credentials are sidecar configuration, loaded once. Failing here
	// fails the agent pod visibly rather than every notification later.
	creds, err := credentialsFromEnv()
	if err != nil {
		return err
	}

	s := &pluginServer{
		client:    req.C().SetTimeout(githubRequestTimeout),
		creds:     creds,
		apiHost:   envOr("gh_url", "api.github.com"),
		tokenFile: *tokenFile,
	}
	srv := &http.Server{
		Addr:              *address,
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if shutdownErr := srv.Shutdown(shutdownCtx); shutdownErr != nil {
			log.Printf("shutting down: %v", shutdownErr)
		}
	}()

	log.Printf("Executor plugin %s listening on %s", pluginName, *address)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serving plugin: %w", err)
	}
	return nil
}

func (s *pluginServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// The agent stops calling a method that answers 404, which suits any
	// method other than the one we implement.
	if r.URL.Path != executePath {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.authorise(r); err != nil {
		log.Printf("refusing call: %v", err)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var args executeTemplateArgs
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBytes)).Decode(&args); err != nil {
		http.Error(w, fmt.Sprintf("decoding request: %v", err), http.StatusBadRequest)
		return
	}

	reply := executeTemplateReply{}
	if raw, ok := args.Template.Plugin[pluginName]; ok {
		node := s.execute(raw)
		log.Printf("%s/%s %s: %s: %s", args.Workflow.Metadata.Namespace, args.Workflow.Metadata.Name,
			args.Template.Name, node.Phase, node.Message)
		reply.Node = &node
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(reply); err != nil {
		log.Printf("writing reply: %v", err)
	}
}

// authorise checks the call carries the agent's token. The token is read
// on each call rather than at startup so the plugin does not depend on
// when the agent's init container writes it.
func (s *pluginServer) authorise(r *http.Request) error {
	data, err := os.ReadFile(s.tokenFile) // #nosec G304 G703 -- operator-supplied configuration.
	if err != nil {
		return fmt.Errorf("reading agent token: %w", err)
	}
	want := "Bearer " + strings.TrimSpace(string(data))
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(want)) != 1 {
		return errors.New("authorization header does not carry the agent token")
	}
	return nil
}

// execute posts the notification a template describes. Failures come
// back as a Failed node rather than an HTTP error: the agent treats most
// HTTP errors as fatal to the call anyway, and a node message is where a
// user will look for why.
func (s *pluginServer) execute(raw json.RawMessage) nodeResult {
	params, err := decodePluginParams(raw)
	if err != nil {
		return nodeResult{Phase: phaseFailed, Message: err.Error()}
	}

	// Record which parameters newNotification asks for, so anything else
	// the template set can be refused. The accepted names then live in one
	// place, and a typo, or an attempt to set gh_url or a credential,
	// fails loudly instead of being ignored.
	used := map[string]bool{}
	get := func(name string) string {
		used[name] = true
		return params[name]
	}
	n, err := newNotification(get, "plugin parameter", s.apiHost)
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(params)) {
		if !used[name] {
			errs = append(errs, fmt.Errorf("unknown plugin parameter %s", name))
		}
	}
	if err = errors.Join(append(errs, err)...); err != nil {
		return nodeResult{Phase: phaseFailed, Message: err.Error()}
	}

	id, err := notify(s.client, n, s.creds)
	if err != nil {
		return nodeResult{Phase: phaseFailed, Message: err.Error()}
	}

	target := fmt.Sprintf("%s/%s@%s", n.organisation, n.repo, n.sha)
	if n.api != apiChecks {
		return nodeResult{Phase: phaseSucceeded, Message: fmt.Sprintf("posted %s status %q to %s", n.state, n.context, target)}
	}
	return nodeResult{
		Phase:   phaseSucceeded,
		Message: fmt.Sprintf("posted %s check run %q (%d) to %s", n.state, n.context, id, target),
		Outputs: &nodeOutputs{Parameters: []nodeParameter{
			{Name: "check_run_id", Value: strconv.FormatInt(id, 10)},
		}},
	}
}

// decodePluginParams reads the template's plugin block into strings.
// Numbers are accepted as well as strings, since YAML turns an unquoted
// check_run_id into one.
func decodePluginParams(raw json.RawMessage) (map[string]string, error) {
	var values map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&values); err != nil {
		return nil, fmt.Errorf("plugin.%s must be an object of parameters: %w", pluginName, err)
	}

	params := make(map[string]string, len(values))
	var errs []error
	for name, value := range values {
		switch v := value.(type) {
		case string:
			params[name] = v
		case json.Number:
			params[name] = v.String()
		default:
			errs = append(errs, fmt.Errorf("plugin parameter %s must be a string", name))
		}
	}
	return params, errors.Join(errs...)
}
