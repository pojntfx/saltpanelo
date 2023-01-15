package services

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/pojntfx/saltpanelo/pkg/utils"
)

var (
	ErrUnauthenticatedRole  = errors.New("unauthenticated role")
	ErrUnauthenticatedRoute = errors.New("unauthenticated route")
)

func SetSwitchCA(sw *Switch, caPEM []byte) {
	sw.caPEM = caPEM
}

type SwitchRemote struct {
	TestLatency      func(ctx context.Context, timeout time.Duration, addrs []string) ([]time.Duration, error)
	TestThroughput   func(ctx context.Context, timeout time.Duration, addrs []string, length, chunks int64) ([]ThroughputResult, error)
	UnprovisionRoute func(ctx context.Context, routeID string) error
	GetPublicIP      func(ctx context.Context) (string, error)
	ProvisionRoute   func(
		ctx context.Context,
		routeID string,
		raddr string,
		switchListenCert,
		switchClientCert,
		adapterListenCert CertPair,
	) ([]string, error)
}

type ThroughputResult struct {
	Read  time.Duration
	Write time.Duration
}

type connPair struct {
	src io.Closer
	dst io.Closer
}

type Switch struct {
	verbose bool
	ahost   string

	routes     map[string]connPair
	routesLock sync.Mutex

	caPEM []byte

	Peers func() map[string]RouterRemote
}

func NewSwitch(verbose bool, ahost string) *Switch {
	return &Switch{
		verbose: verbose,
		ahost:   ahost,

		routes: map[string]connPair{},
	}
}

func (s *Switch) TestLatency(ctx context.Context, timeout time.Duration, addrs []string) ([]time.Duration, error) {
	if s.verbose {
		log.Println("Starting latency tests for addrs", addrs)
	}

	return testLatency(timeout, addrs)
}

func (s *Switch) TestThroughput(ctx context.Context, timeout time.Duration, addrs []string, length, chunks int64) ([]ThroughputResult, error) {
	if s.verbose {
		log.Println("Starting throughput tests for addrs", addrs)
	}

	return testThroughput(timeout, addrs, length, chunks)
}

func (s *Switch) UnprovisionRoute(ctx context.Context, routeID string) error {
	s.routesLock.Lock()
	defer s.routesLock.Unlock()

	if s.verbose {
		log.Println("Unprovisioning route with ID", routeID)
	}

	route, ok := s.routes[routeID]
	if !ok {
		return ErrRouteNotFound
	}

	if err := route.src.Close(); err != nil {
		return err
	}

	if err := route.dst.Close(); err != nil {
		return err
	}

	delete(s.routes, routeID)

	return nil
}

func (s *Switch) GetPublicIP(ctx context.Context) (string, error) {
	if s.verbose {
		log.Println("Getting public IP")
	}

	return s.ahost, nil
}

func (s *Switch) ProvisionRoute(
	ctx context.Context,
	routeID string,
	raddr string,
	switchListenCert,
	switchClientCert,
	adapterListenCert CertPair,
) ([]string, error) {
	if s.verbose {
		log.Println("Provisioning route with ID", routeID, "to raddr", raddr)
	}

	var src net.Conn
	var dst net.Conn

	cp := connPair{}

	ready := make(chan struct{})
	errs := make(chan error)
	addrs := []string{}

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(s.caPEM)

	if strings.TrimSpace(raddr) == "" {
		laddr, err := net.ResolveTCPAddr("tcp", s.ahost+":0")
		if err != nil {
			return []string{}, err
		}

		cer, err := tls.X509KeyPair(adapterListenCert.CertPEM, adapterListenCert.CertPrivKeyPEM)
		if err != nil {
			return []string{}, err
		}

		lis, err := tls.Listen("tcp", laddr.String(), &tls.Config{
			Certificates: []tls.Certificate{cer},
			ClientCAs:    caCertPool,
			ClientAuth:   tls.RequireAndVerifyClientCert,
			VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
				cert := verifiedChains[0][0]

				if cert.Subject.CommonName != utils.RoleAdapterClient {
					return ErrUnauthenticatedRole
				}

				if len(cert.Subject.Country) < 1 || cert.Subject.Country[0] != routeID {
					return ErrUnauthenticatedRoute
				}

				return nil
			},
		})
		if err != nil {
			return []string{}, err
		}

		cp.src = lis
		addrs = append(addrs, lis.Addr().String())

		go func() {
			for {
				rawConn, err := lis.Accept()
				if err != nil {
					if errors.Is(err, net.ErrClosed) {
						return
					}

					if s.verbose {
						log.Println("Could not accept src connection, skipping:", err)
					}

					continue
				}

				conn, ok := rawConn.(*tls.Conn)
				if !ok {
					if s.verbose {
						log.Println("Could not accept non-TLS connection, skipping:", err)
					}

					_ = conn.Close()

					continue
				}

				if err := conn.Handshake(); err != nil {
					if s.verbose {
						log.Println("Could not hanshake TLS connection, skipping:", err)
					}

					_ = conn.Close()

					continue
				}

				src = conn

				ready <- struct{}{}

				break
			}
		}()
	} else {
		cer, err := tls.X509KeyPair(switchClientCert.CertPEM, switchClientCert.CertPrivKeyPEM)
		if err != nil {
			return []string{}, err
		}

		conn, err := tls.Dial("tcp", raddr, &tls.Config{
			RootCAs:      caCertPool,
			Certificates: []tls.Certificate{cer},
		})
		if err != nil {
			return []string{}, err
		}

		cp.src = conn
		src = conn

		go func() {
			ready <- struct{}{}
		}()
	}

	laddr, err := net.ResolveTCPAddr("tcp", s.ahost+":0")
	if err != nil {
		return []string{}, err
	}

	var cer tls.Certificate
	if len(switchListenCert.CertPEM) > 0 {
		cer, err = tls.X509KeyPair(switchListenCert.CertPEM, switchListenCert.CertPrivKeyPEM)
		if err != nil {
			return []string{}, err
		}
	} else {
		cer, err = tls.X509KeyPair(adapterListenCert.CertPEM, adapterListenCert.CertPrivKeyPEM)
		if err != nil {
			return []string{}, err
		}
	}

	lis, err := tls.Listen("tcp", laddr.String(), &tls.Config{
		Certificates: []tls.Certificate{cer},
		ClientCAs:    caCertPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			cert := verifiedChains[0][0]

			if len(switchListenCert.CertPEM) > 0 {
				if cert.Subject.CommonName != utils.RoleSwitchClient {
					return ErrUnauthenticatedRole
				}
			} else {
				if cert.Subject.CommonName != utils.RoleAdapterClient {
					return ErrUnauthenticatedRole
				}
			}

			if len(cert.Subject.Country) < 1 || cert.Subject.Country[0] != routeID {
				return ErrUnauthenticatedRoute
			}

			return nil
		},
	})
	if err != nil {
		return []string{}, err
	}

	cp.dst = lis
	addrs = append(addrs, lis.Addr().String())

	go func() {
		for {
			rawConn, err := lis.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}

				if s.verbose {
					log.Println("Could not accept src connection, skipping:", err)
				}

				continue
			}

			conn, ok := rawConn.(*tls.Conn)
			if !ok {
				if s.verbose {
					log.Println("Could not accept non-TLS connection, skipping:", err)
				}

				_ = conn.Close()

				continue
			}

			if err := conn.Handshake(); err != nil {
				if s.verbose {
					log.Println("Could not hanshake TLS connection, skipping:", err)
				}

				_ = conn.Close()

				continue
			}

			dst = conn

			ready <- struct{}{}

			break
		}
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
			if s.verbose {
				log.Println("Could not accept src or dst connection, stopping:", err)
			}
		}

		go func() {
			defer func() {
				err := recover()

				if s.verbose && err != nil {
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

				if s.verbose && err != nil {
					log.Println("Could not copy from src to dst, stopping:", err)
				}
			}()

			if _, err := io.Copy(dst, src); err != nil {
				panic(err)
			}
		}()
	}()

	s.routesLock.Lock()
	s.routes[routeID] = cp
	s.routesLock.Unlock()

	return addrs, nil
}

func testLatency(timeout time.Duration, addrs []string) ([]time.Duration, error) {
	latencies := []time.Duration{}
	var latencyLock sync.Mutex

	errs := make(chan error)

	var wg sync.WaitGroup

	wg.Add(len(addrs))

	for _, addr := range addrs {
		go func(addr string) {
			defer wg.Done()

			before := time.Now()

			conn, err := net.DialTimeout("tcp", addr, timeout)
			if err != nil {
				errs <- err

				return
			}

			latency := time.Since(before)

			_ = conn.Close()

			latencyLock.Lock()
			latencies = append(latencies, latency)
			latencyLock.Unlock()
		}(addr)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()

		done <- struct{}{}
	}()

	select {
	case err := <-errs:
		return []time.Duration{}, err
	case <-done:
		return latencies, nil
	}
}

func testThroughput(timeout time.Duration, addrs []string, length, chunks int64) ([]ThroughputResult, error) {
	throughputs := []ThroughputResult{}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	for _, addr := range addrs {
		conn, err := net.DialTimeout("tcp", addr, timeout)
		if err != nil {
			return []ThroughputResult{}, err
		}

		throughput := ThroughputResult{}

		{
			before := time.Now()

			for i := int64(0); i < chunks; i++ {
				if _, err := io.CopyN(conn, r, length); err != nil {
					_ = conn.Close()

					return []ThroughputResult{}, err
				}
			}

			throughput.Write = time.Since(before)
		}

		{
			before := time.Now()

			for i := int64(0); i < chunks; i++ {
				if _, err := io.CopyN(io.Discard, conn, length); err != nil {
					_ = conn.Close()

					return []ThroughputResult{}, err
				}
			}

			throughput.Read = time.Since(before)
		}

		_ = conn.Close()

		throughputs = append(throughputs, throughput)
	}

	return throughputs, nil
}
