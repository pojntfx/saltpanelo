package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/pojntfx/saltpanelo/internal/backends"
)

func main() {
	raddr := flag.String("raddr", "ws://localhost:1338", "Gateway remote address")
	ahost := flag.String("ahost", "127.0.0.1", "Host to bind to when receiving calls")
	verbose := flag.Bool("verbose", false, "Whether to enable verbose logging")
	timeout := flag.Int("timeout", 1000, "Milliseconds after which to assume that a call has timed out")

	oidcIssuer := flag.String("oidc-issuer", "", "OIDC issuer (e.g. https://pojntfx.eu.auth0.com/)")
	oidcClientID := flag.String("oidc-client-id", "", "OIDC client ID")
	oidcRedirectURL := flag.String("oidc-redirect-url", "http://localhost:11337", "OIDC redirect URL")

	flag.Parse()

	adapter := backends.NewAdapter(
		func(ctx context.Context, srcID, srcEmail, routeID, channelID string) (bool, error) {
			log.Printf("Call with src ID %v, src email %v, route ID %v and channel ID %v requested and accepted", srcID, srcEmail, routeID, channelID)

			return true, nil
		},
		func(ctx context.Context, routeID string) error {
			log.Println("Call with route ID", routeID, "disconnected")

			return nil
		},
		func(ctx context.Context, routeID, raddr string) error {
			log.Println("Call with route ID", routeID, "and remote address", raddr, "started")

			return nil
		},
		func(url string) error {
			log.Println("Open the following URL in your browser:", url)

			return nil
		},

		*raddr,
		*ahost,
		*verbose,
		*timeout,

		*oidcIssuer,
		*oidcClientID,
		*oidcRedirectURL,
	)

	if err := adapter.Login(); err != nil {
		panic(err)
	}

	go func() {
		if err := adapter.Link(); err != nil {
			panic(err)
		}
	}()

	r := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("Email to call: ")

		email, err := r.ReadString('\n')
		if err != nil {
			panic(err)
		}

		fmt.Print("Channel ID to call: ")

		channelID, err := r.ReadString('\n')
		if err != nil {
			panic(err)
		}

		accepted, err := adapter.RequestCall(email, channelID)
		if err != nil {
			panic(err)
		}

		if accepted {
			log.Println("Callee accepted the call")
		} else {
			log.Println("Callee denied the call")
		}
	}
}
