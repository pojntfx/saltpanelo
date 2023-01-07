package main

import (
	"context"
	"flag"
	"log"
	"net"
	"strings"
	"time"

	"github.com/pojntfx/dudirekta/pkg/rpc"
	"github.com/pojntfx/saltpanelo/pkg/services"
	"github.com/pojntfx/saltpanelo/pkg/utils"
)

func main() {
	raddr := flag.String("raddr", "localhost:1337", "Router remote address")
	laddr := flag.String("laddr", ":1340", "Listen address for latency and throughput tests")
	taddr := flag.String("taddr", "127.0.0.1:1340", "Listen address to advertise for latency and throughput tests")
	ahost := flag.String("ahost", "127.0.0.1", "Host to advertise other switches to dial; leave empty to resolve public IP using STUN")
	timeout := flag.Duration("timeout", time.Minute, "Time after which to assume that a call has timed out")
	verbose := flag.Bool("verbose", false, "Whether to enable verbose logging")
	stunAddr := flag.String("stun", "stun.l.google.com:19302", "STUN server address")

	flag.Parse()

	if strings.TrimSpace(*ahost) == "" {
		ah, err := utils.GetPublicIP(*stunAddr)
		if err != nil {
			panic(err)
		}

		*ahost = ah.String()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	l := services.NewSwitch(*verbose, *ahost)
	clients := 0
	registry := rpc.NewRegistry(
		l,
		services.RouterRemote{},
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
								log.Println("Registering with router with ID", remoteID)
							}

							if err := peer.RegisterSwitch(ctx, *taddr); err != nil {
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

	go func() {
		lis, err := net.Listen("tcp", *laddr)
		if err != nil {
			errs <- err

			return
		}
		defer lis.Close()

		log.Println("Listening on", lis.Addr())

		for {
			func() {
				conn, err := lis.Accept()
				if err != nil {
					log.Println("could not accept connection, continuing:", err)

					return
				}

				go func() {
					defer func() {
						_ = conn.Close()

						if err := recover(); err != nil && !utils.IsClosedErr(err) {
							log.Printf("Client disconnected with error: %v", err)
						}
					}()

					if err := utils.HandleTestConn(*verbose, conn); err != nil {
						panic(err)
					}
				}()
			}()
		}
	}()

	for err := range errs {
		if err == nil {
			return
		}

		panic(err)
	}
}
