package main

import (
	"context"
	"flag"
	"log"
	"net"
	"time"

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

	l := services.NewAdapter(*verbose)
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
					for candidate, peer := range l.Peers() {
						if remoteID == candidate {
							if *verbose {
								log.Println("Registering with router with ID", remoteID)
							}

							if err := peer.RegisterAdapter(ctx); err != nil {
								log.Fatal("Could not register with router with ID", remoteID, ", stopping:", err)
							}

							if *verbose {
								log.Println("Registered with router with ID", remoteID)
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

	errs := make(chan error)

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

	for err := range errs {
		if err == nil {
			return
		}

		panic(err)
	}
}
