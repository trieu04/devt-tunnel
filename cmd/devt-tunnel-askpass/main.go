// Command devt-tunnel-askpass is the program ssh runs for prompts. It forwards each
// prompt to the devt-tunnel app and prints the answer. It holds no secrets itself.
package main

import (
	"fmt"
	"os"

	"github.com/trieu04/devt-tunnel/internal/askpass"
)

func main() {
	// "none" is an informational message from ssh that takes no answer.
	if os.Getenv("SSH_ASKPASS_PROMPT") == "none" {
		return
	}
	endpoint, token := os.Getenv(askpass.EnvEndpoint), os.Getenv(askpass.EnvToken)
	if endpoint == "" || token == "" {
		fmt.Fprintln(os.Stderr, "devt-tunnel-askpass: must be started by devt-tunnel")
		os.Exit(2)
	}
	prompt := ""
	if len(os.Args) > 1 {
		prompt = os.Args[1]
	}
	answer, err := askpass.Ask(endpoint, token, prompt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "devt-tunnel-askpass:", err)
		os.Exit(1)
	}
	fmt.Println(answer)
}
