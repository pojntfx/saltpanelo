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
	timeout := flag.Duration("timeout", time.Minute, "Time after which to assume that a call has timed out")
	verbose := flag.Bool("verbose", false, "Whether to enable verbose logging")
	latencyTestInterval := flag.Duration("latency-test-interval", time.Second*10, "Interval in which to refresh latency values in topology")
	latencyTestTimeout := flag.Duration("latency-test-timeout", time.Second*5, "Dial timeout after which to assume a switch is unreachable from another switch")
	throughputLength := flag.Int64("throughput-length", 1048576, "Length of a single chunk to send for the latency test")
	throughputChunks := flag.Int64("throughput-chunks", 100, "Amount of chunks to send for the latency test")

	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	l := services.NewRouter(
		*verbose,

		*latencyTestInterval,
		*latencyTestTimeout,

		*throughputLength,
		*throughputChunks,
	)
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

	go services.HandleOpen(l)

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
