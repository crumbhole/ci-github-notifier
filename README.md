# ci-github-notifier
A lightweight container to post the status of a CI task to GitHub, allowing GitHub users to see the status of a PR or Branch. Designed for cloud native workflows (eg [Argo Workflows](https://argoproj.github.io/argo-workflows/), or [Tekton](https://tekton.dev/)), but will run wherever a container can be run. Compatible with regular Github and Github Enterprise URLs.

![CI](https://github.com/crumbhole/ci-github-notifier/actions/workflows/ci.yaml/badge.svg) ![Code Quality](https://github.com/crumbhole/ci-github-notifier/actions/workflows/codeql-analysis.yaml/badge.svg) ![Release](https://github.com/crumbhole/ci-github-notifier/actions/workflows/release.yaml/badge.svg)

# Environment Variables
We pass key information to the container using environment variables.
First, we provide the necessary values for the GitHub status API:

| Environment Variable  | Type      | Description                                                                                                                                       |
|---------------------- |---------- |-------------------------------------------------------------------------------------------------------------------------------------------------- |
| `state`               | string    | The state of the status. Can be one of `pending`, `success`, `error`, or `failure` ([Github Docs](https://docs.github.com/en/rest/commits/statuses?apiVersion=2022-11-28#about-commit-statuses)).                                                                       |
| `target_url`          | string    | The target URL to associate with this status. This URL will be linked from the GitHub UI to allow users to easily see the ‘source’ of the Status. |
| `description`         | string    | A short description of the status.                                                                                                                |
| `context`             | string    | A string label to differentiate this status from the status of other systems.                                                                     |

Then we provide GitHub authentication information. We provide one of the following:

| Environment Variable  | Type      | Description                                                                                                                                       |
|---------------------- |---------- |-------------------------------------------------------------------------------------------------------------------------------------------------- |
| `access_token`        | string    | (Optional if `tokenFile` is set): A GitHub Access Token for a user with push access                                                               |
| `tokenFile`           | string    | (Optional if `access_token` is set): Path to a file within the container that contains the GitHub access token. Useful for Vault secrets injection or similar. Takes precedence over `access_token` |

Finally we provide Environment Variables that make up the values of the GitHub API url:

| Environment Variable  | Type      | Description                                                                                                                                       |
|---------------------- |---------- |-------------------------------------------------------------------------------------------------------------------------------------------------- |
| `organisation`        | string    | The GitHub organisation/username for the notification. e.g. given https://github.com/crumbhole/ci-github-notifier, the organisation is "crumbhole"    |
| `app_repo`            | string    | The GitHub repo for the notification. e.g. given https://github.com/crumbhole/ci-github-notifier, the app_repo is "ci-github-notifier"                    |
| `git_sha`             | string    | The SHA1 of the PR or branch you wish to notify                                                                                                               |
| `gh_url`              | string    | (OPTIONAL) The URL of the GitHub API. If omitted, will default to `api.github.com`                                                                              |

# Docker run examples
You are unlikely to want to run these in production using `docker run`, but the following examples give a clear indication of how to execute the container.
## Running with environment variables

```
docker run \
    -e state=pending \
    -e target_url=https://sendible.com \
    -e description="This is an example description" \
    -e context="Example context" \
    -e access_token="123ABC123ABC" \
    -e organisation=crumbhole \
    -e app_repo="ci-github-notifier" \
    -e git_sha="123abc123abc" \
    -e gh_url="api.mydomain.biz" \
    ghcr.io/crumbhole/ci-github-notifier:stable
```

## Mounting tokenFile
```
docker run \
    -e state=pending \
    -e target_url=https://sendible.com \
    -e description="This is an example description" \
    -e context="Example context" \
    -e tokenFile="/tmp/access_token" \
    -e organisation=crumbhole \
    -e app_repo="ci-github-notifier" \
    -e git_sha="123abc123abc" \
    -e gh_url="api.mydomain.biz" \
    -v /path/to/file:/tmp/access_token \
    ghcr.io/crumbhole/ci-github-notifier:stable
```

# Argo Workflows example
A simple Argo Workflows template can be found in the examples directory.
