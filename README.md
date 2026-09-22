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

Then we provide GitHub authentication information, as either a personal access token or GitHub App credentials.

For a personal access token, provide one of the following:

| Environment Variable  | Type      | Description                                                                                                                                       |
|---------------------- |---------- |-------------------------------------------------------------------------------------------------------------------------------------------------- |
| `access_token`        | string    | (Optional if `tokenFile` is set): A GitHub Access Token for a user with push access                                                               |
| `tokenFile`           | string    | (Optional if `access_token` is set): Path to a file within the container that contains the GitHub access token. Useful for Vault secrets injection or similar. `access_token` takes precedence over this |

For a GitHub App, provide `app_id` and one of the two key variables. See [Authenticating as a GitHub App](#authenticating-as-a-github-app) below:

| Environment Variable  | Type      | Description                                                                                                                                       |
|---------------------- |---------- |-------------------------------------------------------------------------------------------------------------------------------------------------- |
| `app_id`              | string    | The GitHub App's ID, from the App's settings page                                                                                                 |
| `app_private_key`     | string    | (Optional if `app_private_key_file` is set): The App's private key, base64 encoded. Encoding keeps the multi-line PEM out of environment variables. Takes precedence over `app_private_key_file` |
| `app_private_key_file`| string    | (Optional if `app_private_key` is set): Path to a file within the container holding the App's private key as a raw PEM. Useful for mounted secrets or Vault injection |

When `app_id` and a key are both set, App credentials are used and `access_token`/`tokenFile` are ignored. Setting only one of `app_id` and a key is an error rather than a silent fall back to the token, so a half-finished migration fails loudly instead of quietly using the old credential.

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

# Authenticating as a GitHub App

A personal access token belongs to a user, expires, and grants whatever that
user can reach. A GitHub App is scoped to the repositories it is installed on
and the permissions it was granted, and the token it uses lasts an hour. When
`app_id` and a private key are supplied, ci-github-notifier mints one of those
short-lived installation tokens itself, so nothing long-lived has to be stored
in your CI system.

Setting one up:

1. Create a GitHub App (organisation settings, or your user settings for a
   personal repo). It needs no webhook and no callback URL.
2. Under **Repository permissions**, grant **Commit statuses: Read and write**.
   Nothing else is required to post statuses.
3. Install the App on the repositories you want to post statuses to. The tool
   looks the installation up from `organisation` and `app_repo`, so there is no
   installation ID to configure.
4. Generate a private key. GitHub downloads a `.pem` file.
5. Note the **App ID** from the App's settings page.

Supply the key as a mounted file:

```
docker run \
    -e state=pending \
    -e target_url=https://argo-workflows.mydomain.biz \
    -e description="This is an example description" \
    -e context="Example context" \
    -e app_id="123456" \
    -e app_private_key_file="/secrets/app.pem" \
    -e organisation=crumbhole \
    -e app_repo="ci-github-notifier" \
    -e git_sha="123abc123abc" \
    -v /path/to/app.pem:/secrets/app.pem \
    ghcr.io/crumbhole/ci-github-notifier:stable
```

Or base64 encoded, which avoids a volume mount when your CI passes everything
as parameters:

```
export app_private_key=$(base64 -w0 < app.pem)
```

If the App is not installed on the repository you name, the run fails with a
message saying so rather than a bare HTTP error.

The installation token is requested for the single repository named by
`organisation`/`app_repo` and for the `statuses: write` permission alone, rather
than for everything the App holds. If the App is installed across an
organisation, the token this tool uses still reaches only the one repository it
is posting to.

# Argo Workflows example
A simple Argo Workflows template can be found in the examples directory. `simple-example.yml` uses a personal access token; `github-app-example.yml` authenticates as a GitHub App with the private key mounted from a Kubernetes secret.

# Development

Go sources are linted with [golangci-lint](https://golangci-lint.run/) against
`.golangci.yaml`. The same configuration runs locally and in CI, so a clean
`make lint` means a clean CI lint job.

```bash
make lint    # golangci-lint run ./...
make build   # lint, then go build ./...
make test    # go test ./...
```

`make lint` needs golangci-lint v2 on your `PATH`; CI pins the version it
installs in `.github/workflows/ci.yaml`. Keep the two in step when bumping.
