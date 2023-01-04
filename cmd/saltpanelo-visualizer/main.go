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
	networkOut := flag.String("network-out", "saltpanelo-network.svg", "Path to write the network graph to")
	routesOut := flag.String("routes-out", "saltpanelo-routes.svg", "Path to write the active routes graph to")
	command := flag.String("command", "dot -T svg", "Command to pipe the Graphviz output through before writing (an empty command writes Graphviz output directly)")

	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	networkFile, err := os.Create(*networkOut)
	if err != nil {
		panic(err)
	}
	defer networkFile.Close()

	routesFile, err := os.Create(*routesOut)
	if err != nil {
		panic(err)
	}
	defer routesFile.Close()

	l := services.NewVisualizer(
		*verbose,
		networkFile,
		routesFile,
		*command,
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
