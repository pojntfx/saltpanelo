package services

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

var (
	ErrNoPeersFound = errors.New("could not find any peers")
)

func SetAdapterCA(adapter *Adapter, caPEM []byte) {
	adapter.caPEM = caPEM
}

type AdapterRemote struct {
	RequestCall      func(ctx context.Context, srcID, routeID, channelID string) (bool, error)
	TestLatency      func(ctx context.Context, timeout time.Duration, addrs []string, benchmarkClientCert CertPair) ([]time.Duration, error)
	TestThroughput   func(ctx context.Context, timeout time.Duration, addrs []string, length, chunks int64, benchmarkClientCert CertPair) ([]ThroughputResult, error)
	UnprovisionRoute func(ctx context.Context, routeID string) error
	ProvisionRoute   func(
		ctx context.Context,
		routeID string,
		raddr string,
		cert CertPair,
	) error
}

func RequestCall(adapter *Adapter, dstID, channelID string) (bool, string, error) {
	return adapter.requestCall(context.Background(), dstID, channelID)
}

type Adapter struct {
	verbose bool
	ahost   string

	onRequestCall      func(ctx context.Context, srcID, routeID, channelID string) (bool, error)
	onCallDisconnected func(ctx context.Context, routeID string) error
	onHandleCall       func(ctx context.Context, routeID, raddr string) error
	getIDToken         func() (string, error)

	routes     map[string]connPair
	routesLock sync.Mutex

	caPEM []byte

	Peers func() map[string]GatewayRemote
}

func NewAdapter(
	verbose bool,
	ahost string,

	onRequestCall func(ctx context.Context, srcID, routeID, channelID string) (bool, error),
	onCallDisconnected func(ctx context.Context, routeID string) error,
	onHandleCall func(ctx context.Context, routeID, raddr string) error,
	getIDToken func() (string, error),
) *Adapter {
	return &Adapter{
		verbose: verbose,
		ahost:   ahost,

		onRequestCall:      onRequestCall,
		onCallDisconnected: onCallDisconnected,
		onHandleCall:       onHandleCall,
		getIDToken:         getIDToken,

		routes: map[string]connPair{},
	}
}

func (a *Adapter) RequestCall(
	ctx context.Context,
	srcID,
	routeID,
	channelID string,
) (bool, error) {
	if a.verbose {
		log.Println("Remote with ID", srcID, "is requesting a call")
	}

	return a.onRequestCall(ctx, srcID, routeID, channelID)
}

func (a *Adapter) requestCall(
	ctx context.Context,
	dstID string,
	channelID string,
) (bool, string, error) {
	if a.verbose {
		log.Println("Requesting a call with ID", dstID)
	}

	token, err := a.getIDToken()
	if err != nil {
		return false, "", err
	}

	for _, peer := range a.Peers() {
		requestCallResult, err := peer.RequestCall(ctx, token, dstID, channelID)
		if err != nil {
			return false, "", err
		}

		return requestCallResult.Accept, requestCallResult.RouteID, nil
	}

	return false, "", ErrNoPeersFound
}

func (a *Adapter) TestLatency(ctx context.Context, timeout time.Duration, addrs []string, benchmarkClientCert CertPair) ([]time.Duration, error) {
	if a.verbose {
		log.Println("Starting latency tests for addrs", addrs)
	}

	cer, err := tls.X509KeyPair(benchmarkClientCert.CertPEM, benchmarkClientCert.CertPrivKeyPEM)
	if err != nil {
		return []time.Duration{}, err
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(a.caPEM)

	return testLatency(timeout, addrs, &tls.Dialer{
		Config: &tls.Config{
			RootCAs:      caCertPool,
			Certificates: []tls.Certificate{cer},
		},
	})
}

func (a *Adapter) TestThroughput(ctx context.Context, timeout time.Duration, addrs []string, length, chunks int64, benchmarkClientCert CertPair) ([]ThroughputResult, error) {
	if a.verbose {
		log.Println("Starting throughput tests for addrs", addrs)
	}

	cer, err := tls.X509KeyPair(benchmarkClientCert.CertPEM, benchmarkClientCert.CertPrivKeyPEM)
	if err != nil {
		return []ThroughputResult{}, err
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(a.caPEM)

	return testThroughput(timeout, addrs, length, chunks, &tls.Dialer{
		Config: &tls.Config{
			RootCAs:      caCertPool,
			Certificates: []tls.Certificate{cer},
		},
	})
}

func (a *Adapter) UnprovisionRoute(ctx context.Context, routeID string) error {
	a.routesLock.Lock()
	defer a.routesLock.Unlock()

	if a.verbose {
		log.Println("Unprovisioning route with ID", routeID)
	}

	route, ok := a.routes[routeID]
	if !ok {
		return ErrRouteNotFound
	}

	if err := route.src.Close(); err != nil {
		return err
	}

	if err := route.dst.Close(); err != nil {
		return err
	}

	delete(a.routes, routeID)

	return a.onCallDisconnected(ctx, routeID)
}

func (a *Adapter) ProvisionRoute(
	ctx context.Context,
	routeID string,
	raddr string,
	cert CertPair,
) error {
	if a.verbose {
		log.Println("Provisioning route with ID", routeID, "to raddr", raddr)
	}

	var src net.Conn
	var dst net.Conn

	cp := connPair{}

	ready := make(chan struct{})
	errs := make(chan error)

	cer, err := tls.X509KeyPair(cert.CertPEM, cert.CertPrivKeyPEM)
	if err != nil {
		return err
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(a.caPEM)

	conn, err := tls.Dial("tcp", raddr, &tls.Config{
		RootCAs:      caCertPool,
		Certificates: []tls.Certificate{cer},
	})
	if err != nil {
		return err
	}

	cp.src = conn
	src = conn

	go func() {
		ready <- struct{}{}
	}()

	laddr, err := net.ResolveTCPAddr("tcp", a.ahost+":0")
	if err != nil {
		return err
	}

	lis, err := net.ListenTCP("tcp", laddr)
	if err != nil {
		return err
	}
	cp.dst = lis

	go func() {
		conn, err := lis.Accept()
		if err != nil {
			if a.verbose {
				log.Println("Could not accept dst connection, stopping:", err)
			}

			errs <- err

			return
		}

		dst = conn

		ready <- struct{}{}
	}()

	go func() {
		i := 0
	l:
		for {
			select {
			case <-ready:
				i++

				if i == 2 {
					break l
				}
			case err = <-errs:
				break l
			}
		}

		if i < 2 {
			if a.verbose {
				log.Println("Could not accept src or dst connection, stopping:", err)
			}
		}

		go func() {
			defer func() {
				err := recover()

				if a.verbose && err != nil {
					log.Println("Could not copy from dst to src, stopping:", err)
				}
			}()

			if _, err := io.Copy(src, dst); err != nil {
				panic(err)
			}
		}()

		go func() {
			defer func() {
				err := recover()

				if a.verbose && err != nil {
					log.Println("Could not copy from src to dst, stopping:", err)
				}
			}()

			if _, err := io.Copy(dst, src); err != nil {
				panic(err)
			}
		}()
	}()

	a.routesLock.Lock()
	a.routes[routeID] = cp
	a.routesLock.Unlock()

	return a.onHandleCall(ctx, routeID, lis.Addr().String())
}
