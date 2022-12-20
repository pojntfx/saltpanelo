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
	raddr := flag.String("raddr", "localhost:1337", "Remote address")
	timeout := flag.Duration("timeout", time.Second*10, "Time after which to assume that a call has timed out")
	verbose := flag.Bool("verbose", false, "Whether to enable verbose logging")
	stunAddr := flag.String("stun", "stun.l.google.com:19302", "STUN server address")
	aaddr := flag.String("aadr", ":1338", "Switch address to advertise; leave hostname empty to resolve public IP using STUN")

	flag.Parse()

	advertisedAddr, err := net.ResolveTCPAddr("tcp", *aaddr)
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	l := services.NewSwitch(*verbose)
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
					for candidate, peer := range l.Peers() {
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
