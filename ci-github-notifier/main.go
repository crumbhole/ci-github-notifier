// Command ci-github-notifier posts a commit status to the GitHub statuses
// API, so a CI system can report build state back to a pull request.
package main

import (
	"fmt"
	"log"

	"github.com/imroc/req/v3"
)

func main() {
	n, err := notificationFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Notifiying Github: %s:%s\n", n.context, n.state)

	creds, err := credentialsFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	id, err := notify(req.C(), n, creds)
	if err != nil {
		log.Fatal(err)
	}

	if n.api == apiChecks {
		fmt.Println("Check run:", id)
		if err := writeCheckRunID(n.checkRunIDFile, id); err != nil {
			log.Fatal(err)
		}
	}
}
