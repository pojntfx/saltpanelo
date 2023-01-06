package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/ncruces/zenity"
	"github.com/pojntfx/dudirekta/pkg/rpc"
	"github.com/pojntfx/saltpanelo/pkg/services"
)

func main() {
	raddr := flag.String("raddr", "localhost:1338", "Gateway remote address")
	timeout := flag.Duration("timeout", time.Minute, "Time after which to assume that a call has timed out")
	verbose := flag.Bool("verbose", false, "Whether to enable verbose logging")

	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errs := make(chan error)

	var l *services.Adapter
	l = services.NewAdapter(
		*verbose,
		func(ctx context.Context, srcID string) (bool, error) {
			if err := zenity.Question(
				fmt.Sprintf("Incoming call from remote with with ID %v, do you want to answer it?", srcID),
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

					if err := peer.HangupCall(ctx, routeID); err != nil {
						errs <- err

						return
					}
				}(peer)
			}

			return nil
		},
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

							if err := peer.RegisterAdapter(ctx); err != nil {
								errs <- err

								return
							}

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
		conn, err := net.Dial("tcp", *raddr)
		if err != nil {
			errs <- err

			return
		}
		defer conn.Close()

		log.Println("Connected to", conn.RemoteAddr())

		if err := registry.Link(conn); err != nil {
			errs <- err

			return
		}
	}()

	go func() {
		for {
			dstID, err := zenity.Entry("ID to call", zenity.Title("Dialer"))
			if err != nil {
				errs <- err

				return
			}

			for _, peer := range l.Peers() {
				requestCallResult, err := peer.RequestCall(ctx, dstID)
				if err != nil {
					errs <- err

					return
				}

				if requestCallResult.Accept {
					if err := zenity.Info(fmt.Sprintf("Callee answered the call with route ID %v", requestCallResult.RouteID)); err != nil {
						errs <- err

						return
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
