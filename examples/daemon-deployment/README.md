# Web server mode (daemon) deployment

This example demonstrates how to deploy the CI GitHub Notifier in web server mode (daemon) in Kubernetes. You deploy the CI GitHub Notifier as a Kubernetes Deployment and expose it as a Kubernetes Service. Then you can call it from other services in the same Kubernetes cluster (eg a running Argo Workflow).

Call it with:
```bash
curl http://localhost:8080/notify?state=success&target_url=http%3A%2F%2Fexample.com&description=Deployment+successful&context=deployment&tokenFile=/path/to/tokenfile&organisation=myorg&app_repo=myrepo&git_sha=abc123&gh_url=api.github.com
```
