package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"log"
	"strings"
	"time"

	"github.com/pojntfx/dudirekta/pkg/rpc"
	"github.com/pojntfx/saltpanelo/pkg/auth"
	"github.com/pojntfx/saltpanelo/pkg/services"
	"github.com/pojntfx/saltpanelo/pkg/utils"
	"nhooyr.io/websocket"
)

func main() {
	raddr := flag.String("raddr", "ws://localhost:1337", "Router remote address")
	laddr := flag.String("laddr", ":1340", "Listen address for latency and throughput tests")
	taddr := flag.String("taddr", "127.0.0.1:1340", "Listen address to advertise for latency and throughput tests")
	ahost := flag.String("ahost", "127.0.0.1", "Host to advertise other switches to dial; leave empty to resolve public IP using STUN")
	timeout := flag.Duration("timeout", time.Minute, "Time after which to assume that a call has timed out")
	verbose := flag.Bool("verbose", false, "Whether to enable verbose logging")
	stunAddr := flag.String("stun", "stun.l.google.com:19302", "STUN server address")
	oidcIssuer := flag.String("oidc-issuer", "", "OIDC issuer (e.g. https://pojntfx.eu.auth0.com/)")
	oidcClientID := flag.String("oidc-client-id", "", "OIDC client ID")
	oidcClientSecret := flag.String("oidc-client-secret", "", "OIDC client secret")
	oidcAudience := flag.String("oidc-audience", "", "Router OIDC audience (e.g. https://saltpanelo-router)")

	flag.Parse()

	if strings.TrimSpace(*oidcIssuer) == "" {
		panic(auth.ErrEmptyOIDCIssuer)
	}

	if strings.TrimSpace(*oidcClientID) == "" {
		panic(auth.ErrEmptyOIDCClientID)
	}

	if strings.TrimSpace(*oidcClientSecret) == "" {
		panic(auth.ErrEmptyOIDCClientSecret)
	}

	if strings.TrimSpace(*ahost) == "" {
		ah, err := utils.GetPublicIP(*stunAddr)
		if err != nil {
			panic(err)
		}

		*ahost = ah.String()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tm := auth.NewTokenManagerClientCredentials(
		*oidcIssuer,
		*oidcClientID,
		*oidcClientSecret,
		*oidcAudience,

		ctx,
	)

	if err := tm.InitialLogin(); err != nil {
		panic(err)
	}

	errs := make(chan error)

	switchConfigChan := make(chan services.SwitchConfiguration)

	l := services.NewSwitch(*verbose, *ahost)
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
					for candidateID, peer := range l.Peers() {
						if remoteID == candidateID {
							if *verbose {
								log.Println("Registering with router with ID", remoteID)
							}

							token, err := tm.GetIDToken()
							if err != nil {
								errs <- err

								return
							}

							switchConfig, err := peer.RegisterSwitch(ctx, token, *taddr)
							if err != nil {
								log.Fatal("Could not register with router with ID", remoteID, ", stopping:", err)
							}

							services.SetSwitchCA(l, switchConfig.CAPEM)

							switchConfigChan <- switchConfig

							if *verbose {
								log.Println("Registered with router with ID", remoteID)
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

	go func() {
		rawConn, _, err := websocket.Dial(ctx, *raddr, nil)
		if err != nil {
			errs <- err

			return
		}
		conn := websocket.NetConn(ctx, rawConn, websocket.MessageText)
		defer conn.Close()

		log.Println("Connected to", conn.RemoteAddr())

		if err := registry.Link(conn); err != nil {
			errs <- err

			return
		}
	}()

	go func() {
		switchConfig := <-switchConfigChan

		cer, err := tls.X509KeyPair(switchConfig.BenchmarkListenCert.CertPEM, switchConfig.BenchmarkListenCert.CertPrivKeyPEM)
		if err != nil {
			errs <- err

			return
		}

		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(switchConfig.CAPEM)

		lis, err := tls.Listen("tcp", *laddr, &tls.Config{
			Certificates: []tls.Certificate{cer},
			ClientCAs:    caCertPool,
			ClientAuth:   tls.RequireAndVerifyClientCert,
			VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
				cert := verifiedChains[0][0]

				if cert.Subject.CommonName != utils.RoleBenchmarkClient {
					return services.ErrUnauthenticatedRole
				}

				return nil
			},
		})
		if err != nil {
			errs <- err

			return
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

						if err := recover(); err != nil && !utils.IsClosedErr(err) {
							log.Printf("Client disconnected with error: %v", err)
						}
					}()

					if err := utils.HandleTestConn(*verbose, conn, switchConfig.BenchmarkLimit); err != nil {
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
