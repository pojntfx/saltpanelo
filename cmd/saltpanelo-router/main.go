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
	laddr := flag.String("laddr", ":1337", "Listen address")
	timeout := flag.Duration("timeout", time.Second*10, "Time after which to assume that a call has timed out")
	verbose := flag.Bool("verbose", false, "Whether to enable verbose logging")
	rttTestInterval := flag.Duration("rtt-test-interval", time.Second*10, "Interval in which to refresh RTT values in topology")
	rttTestTimeout := flag.Duration("rtt-test-timeout", time.Second*5, "Dial timeout after which to assume a switch is unreachable from another switch")

	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	l := services.NewRouter(*verbose, *rttTestInterval, *rttTestTimeout)
	clients := 0
	registry := rpc.NewRegistry(
		l,
		services.SwitchRemote{},
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

				services.HandleClientDisconnect(l, remoteID)
			},
		},
	)
	l.Peers = registry.Peers

	lis, err := net.Listen("tcp", *laddr)
	if err != nil {
		panic(err)
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

					if err := recover(); err != nil {
						log.Printf("Client disconnected with error: %v", err)
					}
				}()

				if err := registry.Link(conn); err != nil {
					panic(err)
				}
			}()
		}()
	}
}
