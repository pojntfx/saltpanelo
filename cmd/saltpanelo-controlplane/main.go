package main

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pojntfx/dudirekta/pkg/rpc"
	"github.com/pojntfx/saltpanelo/pkg/auth"
	"github.com/pojntfx/saltpanelo/pkg/services"
	"github.com/pojntfx/saltpanelo/pkg/utils"
	"nhooyr.io/websocket"
)

var (
	errInvalidCertificateCount = errors.New("invalid certificate count")
)

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}

	workdir := flag.String("workdir", filepath.Join(home, ".local", "share", "saltpanelo", "var", "lib", "saltpanelo"), "Working directory")
	routerLaddr := flag.String("router-laddr", ":1337", "Router listen address")
	gatewayLaddr := flag.String("gateway-laddr", ":1338", "Gateway listen address")
	metricsLaddr := flag.String("metrics-laddr", ":1339", "Metrics listen address")
	timeout := flag.Duration("timeout", time.Minute, "Time after which to assume that a call has timed out")
	verbose := flag.Bool("verbose", false, "Whether to enable verbose logging")
	testInterval := flag.Duration("test-interval", time.Second*10, "Interval in which to refresh latency values in topology")
	testTimeout := flag.Duration("test-timeout", time.Second*5, "Dial timeout after which to assume a switch is unreachable from another switch")
	caValidity := flag.Duration("ca-validity", time.Hour*24*30*365, "Time until generated CA certificate becomes invalid")
	callCertValidity := flag.Duration("call-cert-validity", time.Hour, "Time until generated certificates for calls become invalid")
	rsaBits := flag.Int("rsa-bits", 2048, "RSA bits to use when generating mTLS private keys")
	benchmarkListenCertValidity := flag.Duration("benchmark-listen-cert-validity", time.Hour*24*30*365, "Time until generated certificates for switch benchmark listeners become invalid")
	benchmarkClientCertValidity := flag.Duration("benchmark-client-cert-validity", time.Minute*5, "Time until generated certificates for benchmark clients become invalid")
	gatewayOIDCIssuer := flag.String("gateway-oidc-issuer", "", "Gateway OIDC issuer (e.g. https://pojntfx.eu.auth0.com/)")
	gatewayOIDCClientID := flag.String("gateway-oidc-client-id", "", "Gateway OIDC client ID")
	routerOIDCIssuer := flag.String("router-oidc-issuer", "", "Router OIDC issuer (e.g. https://pojntfx.eu.auth0.com/)")
	routerOIDCClientID := flag.String("router-oidc-client-id", "", "Router OIDC client ID")
	routerOIDCAudience := flag.String("router-oidc-audience", "", "Router OIDC audience (e.g. https://saltpanelo-router)")
	metricsAuthorizedEmail := flag.String("metrics-authorized-email", "", "Authorized email for metrics (e.g. jean.doe@example.com)")
	benchmarkLimit := flag.Int64("benchmark-length", 1048576*100, "Amount of bytes to stream to benchmark clients before closing connection")

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

	if err := os.MkdirAll(*workdir, os.ModePerm); err != nil {
		panic(err)
	}

	if *verbose {
		log.Println("Generating certificate authority")
	}

	regenerateCertificate := false

	var (
		caPEMPath        = filepath.Join(*workdir, "ca.cert.pem")
		caPrivKeyPEMPath = filepath.Join(*workdir, "ca.key.pem")
	)
	if _, err := os.Stat(caPEMPath); err != nil {
		regenerateCertificate = true
	}
	if _, err := os.Stat(caPrivKeyPEMPath); err != nil {
		regenerateCertificate = true
	}

	var (
		caCfg *x509.Certificate
		caPEM,
		caPrivKeyPEM []byte
		caPrivKey *rsa.PrivateKey
	)

regenerate:
	if regenerateCertificate {
		caCfg, caPEM, caPrivKeyPEM, caPrivKey, err = utils.GenerateCertificateAuthority(*rsaBits, *caValidity)
		if err != nil {
			panic(err)
		}

		if err := os.WriteFile(caPEMPath, caPEM, os.ModePerm); err != nil {
			panic(err)
		}

		if err := os.WriteFile(caPrivKeyPEMPath, caPrivKeyPEM, os.ModePerm); err != nil {
			panic(err)
		}
	} else {
		caPEM, err = os.ReadFile(caPEMPath)
		if err != nil {
			panic(err)
		}

		caPrivKeyPEM, err = os.ReadFile(caPrivKeyPEMPath)
		if err != nil {
			panic(err)
		}

		cer, err := tls.X509KeyPair(caPEM, caPrivKeyPEM)
		if err != nil {
			panic(err)
		}

		if len(cer.Certificate) < 1 {
			panic(errInvalidCertificateCount)
		}

		caCfg, err = x509.ParseCertificate(cer.Certificate[0])
		if err != nil {
			panic(err)
		}

		if caCfg.NotAfter.Before(time.Now()) {
			regenerateCertificate = true

			goto regenerate
		}

		b, _ := pem.Decode(caPrivKeyPEM)
		caPrivKey, err = x509.ParsePKCS1PrivateKey(b.Bytes)
		if err != nil {
			panic(err)
		}
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

		*routerOIDCIssuer,
		*routerOIDCClientID,
		*routerOIDCAudience,

		caCfg,
		caPEM,
		caPrivKey,

		*callCertValidity,
		*benchmarkListenCertValidity,
		*benchmarkClientCertValidity,

		*rsaBits,
		*benchmarkLimit,
	)
	gateway := services.NewGateway(
		*verbose,

		*gatewayOIDCIssuer,
		*gatewayOIDCClientID,

		caCfg,
		caPEM,
		caPrivKey,

		*benchmarkClientCertValidity,

		*rsaBits,
		*benchmarkLimit,
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

		if err := http.Serve(lis, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil && !utils.IsClosedErr(err) {
					w.WriteHeader(http.StatusInternalServerError)

					log.Printf("Client disconnected from metrics with error: %v", err)
				}
			}()

			switch r.Method {
			case http.MethodGet:
				c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
					OriginPatterns: []string{"*"},
				})
				if err != nil {
					panic(err)
				}

				pings := time.NewTicker(time.Second / 2)
				defer pings.Stop()

				httpErrs := make(chan error)
				go func() {
					for range pings.C {
						if err := c.Ping(ctx); err != nil {
							httpErrs <- err

							return
						}
					}
				}()

				conn := websocket.NetConn(ctx, c, websocket.MessageText)
				defer conn.Close()

				go func() {
					if err := metricsRegistry.Link(conn); err != nil {
						httpErrs <- err

						return
					}
				}()

				if err := <-httpErrs; err != nil {
					panic(err)
				}
			default:
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
		})); err != nil {
			panic(err)
		}

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

		if err := http.Serve(lis, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil && !utils.IsClosedErr(err) {
					w.WriteHeader(http.StatusInternalServerError)

					log.Printf("Client disconnected from router with error: %v", err)
				}
			}()

			switch r.Method {
			case http.MethodGet:
				c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
					OriginPatterns: []string{"*"},
				})
				if err != nil {
					panic(err)
				}

				pings := time.NewTicker(time.Second / 2)
				defer pings.Stop()

				httpErrs := make(chan error)
				go func() {
					for range pings.C {
						if err := c.Ping(ctx); err != nil {
							httpErrs <- err

							return
						}
					}
				}()

				conn := websocket.NetConn(ctx, c, websocket.MessageText)
				defer conn.Close()

				go func() {
					if err := routerRegistry.Link(conn); err != nil {
						httpErrs <- err

						return
					}
				}()

				if err := <-httpErrs; err != nil {
					panic(err)
				}
			default:
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
		})); err != nil {
			panic(err)
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

		if err := http.Serve(lis, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil && !utils.IsClosedErr(err) {
					w.WriteHeader(http.StatusInternalServerError)

					log.Printf("Client disconnected from gateway with error: %v", err)
				}
			}()

			switch r.Method {
			case http.MethodGet:
				c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
					OriginPatterns: []string{"*"},
				})
				if err != nil {
					panic(err)
				}

				pings := time.NewTicker(time.Second / 2)
				defer pings.Stop()

				httpErrs := make(chan error)
				go func() {
					for range pings.C {
						if err := c.Ping(ctx); err != nil {
							httpErrs <- err

							return
						}
					}
				}()

				conn := websocket.NetConn(ctx, c, websocket.MessageText)
				defer conn.Close()

				go func() {
					if err := gatewayRegistry.Link(conn); err != nil {
						httpErrs <- err

						return
					}
				}()

				if err := <-httpErrs; err != nil {
					panic(err)
				}
			default:
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
		})); err != nil {
			panic(err)
		}
	}()

	for err := range errs {
		if err == nil {
			return
		}

		panic(err)
	}
}
