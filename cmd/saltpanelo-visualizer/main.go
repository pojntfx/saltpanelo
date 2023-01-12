package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/cli/browser"
	"github.com/pojntfx/dudirekta/pkg/rpc"
	"github.com/pojntfx/saltpanelo/pkg/auth"
	"github.com/pojntfx/saltpanelo/pkg/services"
)

func main() {
	raddr := flag.String("raddr", "localhost:1339", "Metric remote address")
	timeout := flag.Duration("timeout", time.Minute, "Time after which to assume that a call has timed out")
	verbose := flag.Bool("verbose", false, "Whether to enable verbose logging")
	networkOut := flag.String("network-out", "saltpanelo-network.svg", "Path to write the network graph to")
	routesOut := flag.String("routes-out", "saltpanelo-routes.svg", "Path to write the active routes graph to")
	command := flag.String("command", "dot -T svg", "Command to pipe the Graphviz output through before writing (an empty command writes Graphviz output directly)")

	oidcIssuer := flag.String("oidc-issuer", "", "OIDC issuer (e.g. https://pojntfx.eu.auth0.com/)")
	oidcClientID := flag.String("oidc-client-id", "", "OIDC client ID")
	oidcRedirectURL := flag.String("oidc-redirect-url", "http://localhost:11337", "OIDC redirect URL")

	flag.Parse()

	if strings.TrimSpace(*oidcIssuer) == "" {
		panic(auth.ErrEmptyOIDCIssuer)
	}

	if strings.TrimSpace(*oidcClientID) == "" {
		panic(auth.ErrEmptyOIDCClientID)
	}

	if strings.TrimSpace(*oidcRedirectURL) == "" {
		panic(auth.ErrEmptyOIDCRedirectURL)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tm := auth.NewTokenManagerAuthorizationCode(
		*oidcIssuer,
		*oidcClientID,
		*oidcRedirectURL,

		func(s string) error {
			if err := browser.OpenURL(s); err != nil {
				log.Printf(`Could not open browser, please open the following URL in your browser manually to authorize:
%v`, s)
			}

			return nil
		},

		ctx,
	)

	if err := tm.InitialLogin(); err != nil {
		panic(err)
	}

	networkFile, err := os.Create(*networkOut)
	if err != nil {
		panic(err)
	}
	defer networkFile.Close()

	routesFile, err := os.Create(*routesOut)
	if err != nil {
		panic(err)
	}
	defer routesFile.Close()

	l := services.NewVisualizer(
		*verbose,
		networkFile,
		routesFile,
		*command,
		tm.GetIDToken,
	)
	clients := 0
	registry := rpc.NewRegistry(
		l,
		services.MetricsRemote{},
		*timeout,
		ctx,
		&rpc.Options{
			ResponseBufferLen: rpc.DefaultResponseBufferLen,
			OnClientConnect: func(remoteID string) {
				clients++

				log.Printf("%v clients connected", clients)
			},
			OnClientDisconnect: func(remoteID string) {
				clients--

				log.Printf("%v clients connected", clients)
			},
		},
	)
	l.Peers = registry.Peers

	conn, err := net.Dial("tcp", *raddr)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	log.Println("Connected to", conn.RemoteAddr())

	if err := registry.Link(conn); err != nil {
		panic(err)
	}
}
