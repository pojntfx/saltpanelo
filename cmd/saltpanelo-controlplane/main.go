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
	routerLaddr := flag.String("router-laddr", ":1337", "Router listen address")
	gatewayLaddr := flag.String("gateway-laddr", ":1338", "Gateway listen address")
	metricsLaddr := flag.String("metrics-laddr", ":1339", "Metrics listen address")
	timeout := flag.Duration("timeout", time.Minute, "Time after which to assume that a call has timed out")
	verbose := flag.Bool("verbose", false, "Whether to enable verbose logging")
	latencyTestInterval := flag.Duration("latency-test-interval", time.Second*10, "Interval in which to refresh latency values in topology")
	latencyTestTimeout := flag.Duration("latency-test-timeout", time.Second*5, "Dial timeout after which to assume a switch is unreachable from another switch")
	throughputLength := flag.Int64("throughput-length", 1048576, "Length of a single chunk to send for the latency test")
	throughputChunks := flag.Int64("throughput-chunks", 100, "Amount of chunks to send for the latency test")

	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := services.NewMetrics(*verbose)
	metricsClients := 0
	metricsRegistry := rpc.NewRegistry(
		metrics,
		services.VisualizerRemote{},
		*timeout,
		ctx,
		&rpc.Options{
			ResponseBufferLen: rpc.DefaultResponseBufferLen,
			OnClientConnect: func(remoteID string) {
				metricsClients++

				log.Printf("%v clients connected to metrics", metricsClients)
			},
			OnClientDisconnect: func(remoteID string) {
				metricsClients--

				log.Printf("%v clients connected to metrics", metricsClients)
			},
		},
	)
	metrics.Peers = metricsRegistry.Peers

	router := services.NewRouter(
		*verbose,

		*latencyTestInterval,
		*latencyTestTimeout,

		*throughputLength,
		*throughputChunks,
	)
	routerClients := 0
	routerRegistry := rpc.NewRegistry(
		router,
		services.SwitchRemote{},
		*timeout,
		ctx,
		&rpc.Options{
			ResponseBufferLen: rpc.DefaultResponseBufferLen,
			OnClientConnect: func(remoteID string) {
				routerClients++

				log.Printf("%v clients connected to router", routerClients)
			},
			OnClientDisconnect: func(remoteID string) {
				routerClients--

				log.Printf("%v clients connected to router", routerClients)

				services.HandleRouterClientDisconnect(router, remoteID)
			},
		},
	)
	router.Peers = routerRegistry.Peers
	router.Metrics = metrics

	gateway := services.NewGateway(
		*verbose,
	)
	gatewayClients := 0
	gatewayRegistry := rpc.NewRegistry(
		gateway,
		services.AdapterRemote{},
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

				services.HandleGatewayClientDisconnect(gateway, remoteID)
			},
		},
	)
	gateway.Peers = gatewayRegistry.Peers
	gateway.Router = router
	router.Gateway = gateway

	go services.HandleRouterOpen(router)

	errs := make(chan error)

	go func() {
		lis, err := net.Listen("tcp", *metricsLaddr)
		if err != nil {
			errs <- err

			return
		}
		defer lis.Close()

		log.Println("Metrics listening on", lis.Addr())

		for {
			func() {
				conn, err := lis.Accept()
				if err != nil {
					log.Println("could not accept metrics connection, continuing:", err)

					return
				}

				go func() {
					defer func() {
						_ = conn.Close()

						if err := recover(); err != nil && !utils.IsClosedErr(err) {
							log.Printf("Client disconnected from metrics with error: %v", err)
						}
					}()

					if err := metricsRegistry.Link(conn); err != nil {
						panic(err)
					}
				}()
			}()
		}
	}()

	go func() {
		lis, err := net.Listen("tcp", *routerLaddr)
		if err != nil {
			errs <- err

			return
		}
		defer lis.Close()

		log.Println("Router listening on", lis.Addr())

		for {
			func() {
				conn, err := lis.Accept()
				if err != nil {
					log.Println("could not accept router connection, continuing:", err)

					return
				}

				go func() {
					defer func() {
						_ = conn.Close()

						if err := recover(); err != nil && !utils.IsClosedErr(err) {
							log.Printf("Client disconnected from router with error: %v", err)
						}
					}()

					if err := routerRegistry.Link(conn); err != nil {
						panic(err)
					}
				}()
			}()
		}
	}()

	go func() {
		lis, err := net.Listen("tcp", *gatewayLaddr)
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
					log.Println("could not accept gateway connection, continuing:", err)

					return
				}

				go func() {
					defer func() {
						_ = conn.Close()

						if err := recover(); err != nil && !utils.IsClosedErr(err) {
							log.Printf("Client disconnected from gateway with error: %v", err)
						}
					}()

					if err := gatewayRegistry.Link(conn); err != nil {
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
