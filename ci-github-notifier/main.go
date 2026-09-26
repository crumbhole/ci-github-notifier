// Command ci-github-notifier posts a commit status or check run to
// GitHub, so a CI system can report build state back to a pull request.
//
// By default it posts one notification configured by environment
// variables and exits. Run as "ci-github-notifier plugin" it is instead
// an Argo Workflows executor plugin, posting one notification per call.
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/imroc/req/v3"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "plugin" {
		if err := runPlugin(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}

	n, err := notificationFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Notifying GitHub: %s:%s\n", n.context, n.state)

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
