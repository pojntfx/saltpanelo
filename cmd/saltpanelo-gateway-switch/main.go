package main

import (
	"context"
	"flag"
	"log"
	"net"
	"time"

	"github.com/pojntfx/dudirekta/pkg/rpc"
	"github.com/pojntfx/saltpanelo/pkg/services"
	"github.com/pojntfx/saltpanelo/pkg/utils"
)

func main() {
	raddr := flag.String("raddr", "localhost:1337", "Remote address (of router)")
	laddr := flag.String("laddr", ":1339", "Listen address (of gateway)")
	saddr := flag.String("saddr", ":1338", "Address (of switch) to advertise; leave hostname empty to resolve public IP using STUN")
	timeout := flag.Duration("timeout", time.Second*10, "Time after which to assume that a call has timed out")
	verbose := flag.Bool("verbose", false, "Whether to enable verbose logging")
	stunAddr := flag.String("stun", "stun.l.google.com:19302", "STUN server address")

	flag.Parse()

	advertisedAddr, err := net.ResolveTCPAddr("tcp", *saddr)
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := services.NewSwitch(*verbose)
	switchClients := 0
	switchRegistry := rpc.NewRegistry(
		s,
		services.RouterRemote{},
		*timeout,
		ctx,
		&rpc.Options{
			ResponseBufferLen: rpc.DefaultResponseBufferLen,
			OnClientConnect: func(remoteID string) {
				switchClients++

				log.Printf("%v clients connected to switch", switchClients)

				go func() {
					for candidate, peer := range s.Peers() {
						if remoteID == candidate {
							if *verbose {
								log.Println("Registering with switch with ID", remoteID)
							}

							if advertisedAddr.IP == nil {
								advertisedAddr.IP, err = utils.GetPublicIP(*stunAddr)

								if err != nil {
									log.Fatal("Could not get public IP using STUN, stopping:", err)
								}
							}

							if err := peer.RegisterSwitch(ctx, advertisedAddr.String()); err != nil {
								log.Fatal("Could not register with switch with ID", remoteID, ", stopping:", err)
							}

							if *verbose {
								log.Println("Registered with switch with ID", remoteID)
							}
						}
					}
				}()
			},
			OnClientDisconnect: func(remoteID string) {
				switchClients--

				log.Printf("%v clients connected to switch", switchClients)
			},
		},
	)
	s.Peers = switchRegistry.Peers

	g := services.NewGateway(*verbose)
	gatewayClients := 0
	gatewayRegistry := rpc.NewRegistry(
		g,
		services.RouterRemote{},
		*timeout,
		ctx,
		&rpc.Options{
			ResponseBufferLen: rpc.DefaultResponseBufferLen,
			OnClientConnect: func(remoteID string) {
				gatewayClients++

				log.Printf("%v clients connected to gateway", gatewayClients)
			},
			OnClientDisconnect: func(remoteID string) {
				gatewayClients--

				log.Printf("%v clients connected to gateway", gatewayClients)
			},
		},
	)
	s.Peers = gatewayRegistry.Peers

	errs := make(chan error)

	go func() {
		conn, err := net.Dial("tcp", *raddr)
		if err != nil {
			errs <- err

			return
		}
		defer conn.Close()

		log.Println("Switch connected to", conn.RemoteAddr())

		if err := switchRegistry.Link(conn); err != nil {
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

		log.Println("Gateway listening on", lis.Addr())

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

						if err := recover(); err != nil {
							log.Printf("Client disconnected with error: %v", err)
						}
					}()

					if err := gatewayRegistry.Link(conn); err != nil {
						errs <- err

						return
					}
				}()
			}()
		}
	}()

	for range errs {
		panic(err)
	}
}
