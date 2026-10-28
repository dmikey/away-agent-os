//go:build darwin && cgo

// agentcheck exercises a provider contract with the existing Keychain connection.
// It never prints, exports or writes the credential and never visits a task URL.
package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/dmikey/away-agent-os/desktop/internal/credentials"
	"github.com/dmikey/away-agent-os/desktop/internal/provider"
	"io"
	"os"
	"time"
)

func main() {
	live := flag.Bool("live", false, "explicitly allow one provider API request using the saved OpenAI connection")
	flag.Parse()
	if !*live {
		fmt.Fprintln(os.Stderr, "Use -live to permit a single provider request. Input is the JSON API body on stdin.")
		os.Exit(2)
	}
	vault := credentials.Keychain{}
	if !vault.Has("OpenAI") {
		fmt.Fprintln(os.Stderr, "No saved OpenAI connection is available.")
		os.Exit(1)
	}
	body, e := io.ReadAll(io.LimitReader(os.Stdin, 1024*1024))
	if e != nil {
		fmt.Fprintln(os.Stderr, "Could not read test request")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	response, e := (provider.Client{Vault: vault}).Complete(ctx, "OpenAI", "", string(body))
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	if response.Status != 200 {
		fmt.Fprintln(os.Stderr, "Provider test failed with status", response.Status)
		os.Exit(1)
	}
	fmt.Println(response.Body)
}
