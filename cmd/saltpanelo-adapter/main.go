package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/cli/browser"
	"github.com/ncruces/zenity"
	"github.com/pojntfx/dudirekta/pkg/rpc"
	"github.com/pojntfx/saltpanelo/pkg/auth"
	"github.com/pojntfx/saltpanelo/pkg/services"
	"nhooyr.io/websocket"
)

func main() {
	raddr := flag.String("raddr", "ws://localhost:1338", "Gateway remote address")
	ahost := flag.String("ahost", "127.0.0.1", "Host to bind to when receiving calls")
	timeout := flag.Duration("timeout", time.Minute, "Time after which to assume that a call has timed out")
	verbose := flag.Bool("verbose", false, "Whether to enable verbose logging")

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

	errs := make(chan error)

	var l *services.Adapter
	l = services.NewAdapter(
		*verbose,
		*ahost,
		func(ctx context.Context, srcID, srcEmail, routeID, channelID string) (bool, error) {
			if err := zenity.Question(
				fmt.Sprintf("Incoming call from remote with with ID %v, email %v, route ID %v and channel ID %v, do you want to answer it?", srcID, srcEmail, routeID, channelID),
				zenity.Title("Incoming Call"),
				zenity.QuestionIcon,
				zenity.OKLabel("Answer"),
				zenity.CancelLabel("Decline"),
			); err != nil {
				if errors.Is(err, zenity.ErrCanceled) {
					return false, nil
				}

				return false, err
			}

			return true, nil
		},
		func(ctx context.Context, routeID string) error {
			go func() {
				if err := zenity.Info(fmt.Sprintf("Call with route ID %v ended", routeID)); err != nil {
					log.Println("Could not display call disconnection message, continuing:", err)
				}
			}()

			return nil
		},
		func(ctx context.Context, routeID, raddr string) error {
			for _, peer := range l.Peers() {
				go func(peer services.GatewayRemote) {
					if err := zenity.Question(
						fmt.Sprintf("Call with route ID %v listening on address %v", routeID, raddr),
						zenity.Title("Ongoing Call"),
						zenity.QuestionIcon,
						zenity.OKLabel("Hang Up"),
						zenity.NoCancel(),
					); err != nil {
						if errors.Is(err, zenity.ErrCanceled) {
							return
						}

						errs <- err

						return
					}

					if *verbose {
						log.Println("Hanging up call with route ID", routeID)
					}

					token, err := tm.GetIDToken()
					if err != nil {
						errs <- err

						return
					}

					if err := peer.HangupCall(ctx, token, routeID); err != nil {
						errs <- err

						return
					}
				}(peer)
			}

			return nil
		},
		tm.GetIDToken,
	)
	clients := 0
	registry := rpc.NewRegistry(
		l,
		services.GatewayRemote{},
		*timeout,
		ctx,
		&rpc.Options{
			ResponseBufferLen: rpc.DefaultResponseBufferLen,
			OnClientConnect: func(remoteID string) {
				clients++

				log.Printf("%v clients connected", clients)

				go func() {
					for candidateID, peer := range l.Peers() {
						if remoteID == candidateID {
							if *verbose {
								log.Println("Registering with gateway with ID", remoteID)
							}

							token, err := tm.GetIDToken()
							if err != nil {
								errs <- err

								return
							}

							caPEM, err := peer.RegisterAdapter(ctx, token)
							if err != nil {
								errs <- err

								return
							}

							services.SetAdapterCA(l, caPEM)

							if *verbose {
								log.Println("Registered with gateway with ID", remoteID)
							}
						}
					}
				}()
			},
			OnClientDisconnect: func(remoteID string) {
				clients--

				log.Printf("%v clients connected", clients)
			},
		},
	)
	l.Peers = registry.Peers

	go func() {
		rawConn, _, err := websocket.Dial(ctx, *raddr, nil)
		if err != nil {
			errs <- err

			return
		}
		conn := websocket.NetConn(ctx, rawConn, websocket.MessageText)
		defer conn.Close()

		log.Println("Connected to", conn.RemoteAddr())

		if err := registry.Link(conn); err != nil {
			errs <- err

			return
		}
	}()

	go func() {
		for {
			email, err := zenity.Entry("Email to call", zenity.Title("Dialer"))
			if err != nil {
				errs <- err

				return
			}

			token, err := tm.GetIDToken()
			if err != nil {
				errs <- err

				return
			}

			for _, peer := range l.Peers() {
				dstID, err := peer.ResolveEmailToID(ctx, token, email)
				if err != nil {
					errs <- err

					return
				}

				channelID, err := zenity.Entry("Channel ID to call", zenity.Title("Dialer"))
				if err != nil {
					errs <- err

					return
				}

				requestCallResult, err := peer.RequestCall(ctx, token, dstID, channelID)
				if err != nil {
					errs <- err

					return
				}

				if requestCallResult.Accept {
					if *verbose {
						log.Println("Callee answered the call with route ID", requestCallResult.RouteID)
					}
				} else {
					if err := zenity.Error("Callee declined the call"); err != nil {
						errs <- err

						return
					}
				}
			}
		}
	}()

	for err := range errs {
		if err == nil {
			return
		}

		panic(err)
	}
}
