package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"time"

	"github.com/pojntfx/dudirekta/pkg/rpc"
	"github.com/pojntfx/saltpanelo/pkg/services"
)

func main() {
	raddr := flag.String("raddr", "localhost:1339", "Metric remote address")
	timeout := flag.Duration("timeout", time.Minute, "Time after which to assume that a call has timed out")
	verbose := flag.Bool("verbose", false, "Whether to enable verbose logging")
	out := flag.String("out", "saltpanelo.gv", "Path to write the Graphviz file to")

	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	file, err := os.Create(*out)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	l := services.NewVisualizer(
		*verbose,
		file,
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
