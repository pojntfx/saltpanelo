package main

import (
	"context"
	"flag"
	"log"
	"net"
	"strings"
	"time"

	"github.com/pojntfx/dudirekta/pkg/rpc"
	"github.com/pojntfx/saltpanelo/pkg/auth"
	"github.com/pojntfx/saltpanelo/pkg/services"
	"github.com/pojntfx/saltpanelo/pkg/utils"
)

func main() {
	routerLaddr := flag.String("router-laddr", ":1337", "Router listen address")
	gatewayLaddr := flag.String("gateway-laddr", ":1338", "Gateway listen address")
	metricsLaddr := flag.String("metrics-laddr", ":1339", "Metrics listen address")
	timeout := flag.Duration("timeout", time.Minute, "Time after which to assume that a call has timed out")
	verbose := flag.Bool("verbose", false, "Whether to enable verbose logging")
	testInterval := flag.Duration("test-interval", time.Second*10, "Interval in which to refresh latency values in topology")
	testTimeout := flag.Duration("test-timeout", time.Second*5, "Dial timeout after which to assume a switch is unreachable from another switch")
	throughputLength := flag.Int64("throughput-length", 1048576, "Length of a single chunk to send for the latency test")
	throughputChunks := flag.Int64("throughput-chunks", 100, "Amount of chunks to send for the latency test")
	caValidity := flag.Duration("ca-validity", time.Hour*24*30*365, "Time until generated CA certificate becomes invalid")
	certValidity := flag.Duration("cert-validity", time.Hour, "Time until generated certificates for calls become invalid")
	gatewayOIDCIssuer := flag.String("gateway-oidc-issuer", "", "Gateway OIDC issuer (e.g. https://pojntfx.eu.auth0.com/)")
	gatewayOIDCClientID := flag.String("gateway-oidc-client-id", "", "Gateway OIDC client ID")
	routerOIDCIssuer := flag.String("router-oidc-issuer", "", "Router OIDC issuer (e.g. https://pojntfx.eu.auth0.com/)")
	routerOIDCClientID := flag.String("router-oidc-client-id", "", "Router OIDC client ID")
	routerOIDCAudience := flag.String("router-oidc-audience", "", "Router OIDC audience (e.g. https://saltpanelo-router)")
	metricsAuthorizedEmail := flag.String("metrics-authorized-email", "", "Authorized email for metrics (e.g. jean.doe@example.com)")

	flag.Parse()

	if strings.TrimSpace(*gatewayOIDCIssuer) == "" {
		panic(auth.ErrEmptyOIDCIssuer)
	}

	if strings.TrimSpace(*gatewayOIDCClientID) == "" {
		panic(auth.ErrEmptyOIDCClientID)
	}

	if strings.TrimSpace(*metricsAuthorizedEmail) == "" {
		panic(auth.ErrEmptyMetricsAuthorizedEmail)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if *verbose {
		log.Println("Generating certificate authority")
	}

	caCfg, caPEM, caPrivKeyPEM, err := utils.GenerateCertificateAuthority(*caValidity)
	if err != nil {
		panic(err)
	}

	metrics := services.NewMetrics(
		*verbose,
		*gatewayOIDCIssuer,
		*gatewayOIDCClientID,
		*metricsAuthorizedEmail,
	)
	router := services.NewRouter(
		*verbose,

		*testInterval,
		*testTimeout,

		*throughputLength,
		*throughputChunks,

		*routerOIDCIssuer,
		*routerOIDCClientID,
		*routerOIDCAudience,

		caCfg,
		caPEM,
		caPrivKeyPEM,

		*certValidity,
	)
	gateway := services.NewGateway(
		*verbose,

		*gatewayOIDCIssuer,
		*gatewayOIDCClientID,

		caPEM,
	)

	if err := metrics.Open(ctx); err != nil {
		panic(err)
	}

	if err := router.Open(ctx); err != nil {
		panic(err)
	}

	if err := gateway.Open(ctx); err != nil {
		panic(err)
	}

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

				if err := services.HandleMetricsClientConnect(router); err != nil {
					log.Println("Could not handle router client disconnected, continuing:", err)
				}
			},
			OnClientDisconnect: func(remoteID string) {
				metricsClients--

				log.Printf("%v clients connected to metrics", metricsClients)
			},
		},
	)
	metrics.Peers = metricsRegistry.Peers

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

				if err := services.HandleRouterClientDisconnect(router, gateway, remoteID); err != nil {
					log.Println("Could not handle router client disconnected, continuing:", err)
				}
			},
		},
	)
	router.Peers = routerRegistry.Peers
	router.Metrics = metrics

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

				if err := services.HandleGatewayClientDisconnect(router, gateway, remoteID); err != nil {
					log.Println("Could not handle gateway client disconnected, continuing:", err)
				}
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
