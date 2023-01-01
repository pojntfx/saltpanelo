package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"os"
	"time"

	"github.com/pojntfx/dudirekta/pkg/rpc"
	"github.com/pojntfx/saltpanelo/pkg/services"
	"golang.org/x/exp/slices"
)

var (
	allowlistedFormats = []string{"dot", "svg", "png", "jpg"}

	errInvalidFormat = errors.New("could not continue with unsupported format")
)

func main() {
	raddr := flag.String("raddr", "localhost:1339", "Metric remote address")
	timeout := flag.Duration("timeout", time.Minute, "Time after which to assume that a call has timed out")
	verbose := flag.Bool("verbose", false, "Whether to enable verbose logging")
	out := flag.String("out", "saltpanelo.svg", "Path to write the graph to")
	format := flag.String("format", "svg", "Format to render as (dot, svg, png or jpg)")

	flag.Parse()

	if !slices.Contains(allowlistedFormats, *format) {
		panic(errInvalidFormat)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	file, err := os.Create(*out)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	l := services.NewVisualizer(
		*verbose,
		*format,
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
