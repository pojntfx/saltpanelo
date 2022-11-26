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

	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	registry := rpc.NewRegistry(
		services.Router{},
		struct{}{},
		*timeout,
		ctx,
	)

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
				return
			}

			clients := 0

			go func() {
				clients++

				log.Printf("%v clients connected", clients)

				defer func() {
					clients--

					_ = conn.Close()

					if err := recover(); err != nil {
						log.Printf("Client disconnected with error: %v", err)
					}

					log.Printf("%v clients connected", clients)
				}()

				if err := registry.Link(conn); err != nil {
					panic(err)
				}
			}()
		}()
	}
}
