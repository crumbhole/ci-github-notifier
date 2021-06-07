# ci-github-notifier
A lightweight container to post the status of a CI task to GitHub, allowing GitHub users to see the status of a PR or Branch.

# API information 
A repository status notification message to GitHub consists of four elements

| Name 	| Type |	Description|
|----   |----   |   ----    |
| `state` | string | The state of the status. Can be one of pending, success, error, or failure. |
| `target_url` |  string | The target URL to associate with this status. This URL will be linked from the GitHub UI to allow users to easily see the ‘source’ of the Status. |
| `description` |	string 	| A short description of the status. |
| `context` | string | A string label to differentiate this status from the status of other systems. Default: "default" |

# Environment Variables
We must set up the following environment variables for the container to work

| Variable                      | Description                               | 
| ----------------------------- | ----------------------------------------- | 
| `state`                       | The state of the status. Can be one of pending, success, error, or failure.                                                     |
| `target_url`                  | The target URL to associate with this status. This URL will be linked from the GitHub UI to allow users to easily see the ‘source’ of the Status. Must start with 'http' or 'https'                  |
| `description`                 | A short description of the status.                   |
| `context`                     | context of message to github              |
|   | |
| `access_token`                | Optional (if `tokenFile` is set): A GitHub Access Token for a user with push access                                                           |
| `tokenFile`                   | Optional (if `access_token` is set): Path to a file within the container that contains the GitHub access token. Useful for Vault secrets injection or similar. Takes precedence over `access_token` |
|   | |
| `organisation`                | The GitHub organisation for the notification                                |
| `app_repo`                    | the GitHub repo for the notification                            |
| `git_sha`                     | The SHA1 of the PR or branch              |


# Docker run examples
## Running with environment variables

```
docker run \
    -e state=pending \
    -e target_url=https://sendible.com \
    -e description="This is an example description" \
    -e context="Example context" \
    -e access_token="123ABC123ABC" \
    -e organisation=sendible-labs \
    -e app_repo="ci-github-notifier" \
    -e git_sha="123abc123abc" \
    ghcr.io/sendible-labs/ci-github-notifier:stable
```

## Mounting tokenFile
```
docker run \
    -e state=pending \
    -e target_url=https://sendible.com \
    -e description="This is an example description" \
    -e context="Example context" \
    -e tokenFile="/tmp/access_token" \
    -e organisation=sendible-labs \
    -e app_repo="ci-github-notifier" \
    -e git_sha="123abc123abc" \
    -v /path/to/file:/tmp/access_token \
    ghcr.io/sendible-labs/ci-github-notifier:stable
```

# Argo Workflows example
todo