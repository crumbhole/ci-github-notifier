<!-- This is an auto-generated file. DO NOT EDIT -->
# ci-github-notifier

* Needs: >= v3.3.1
* Image: ghcr.io/crumbhole/ci-github-notifier:stable

Posts commit statuses or check runs to GitHub from a workflow, without starting a pod per notification. Credentials are configured once, on this sidecar, from the ci-github-notifier secret.

Install:

    kubectl apply -f ci-github-notifier-executor-plugin-configmap.yaml

Uninstall:
	
    kubectl delete cm ci-github-notifier-executor-plugin 
