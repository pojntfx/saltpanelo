package services

import (
	"context"
	"io"
	"log"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"
)

type SwitchRemote struct {
	TestLatency      func(ctx context.Context, timeout time.Duration, addrs []string) ([]time.Duration, error)
	TestThroughput   func(ctx context.Context, timeout time.Duration, addrs []string, length, chunks int64) ([]ThroughputResult, error)
	UnprovisionRoute func(ctx context.Context, routeID string) error
	ProvisionRoute   func(ctx context.Context, routeID string, raddr string) (int, error)
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

func (s *Switch) ProvisionRoute(ctx context.Context, routeID string, raddr string) (int, error) {
	if s.verbose {
		log.Println("Provisioning route with ID", routeID, "to raddr", raddr)
	}

	var src net.Conn
	var dst net.Conn

	cp := connPair{}

	ready := make(chan struct{})

	if strings.TrimSpace(raddr) == "" {
		laddr, err := net.ResolveTCPAddr("tcp", s.ahost+":0")
		if err != nil {
			return -1, err
		}

		lis, err := net.ListenTCP("tcp", laddr)
		if err != nil {
			return -1, err
		}
		cp.src = lis

		go func() {
			conn, err := lis.Accept()
			if err != nil {
				if s.verbose {
					log.Println("Could not accept src connection, stopping:", err)
				}

				close(ready)

				return
			}

			src = conn

			ready <- struct{}{}
		}()
	} else {
		conn, err := net.Dial("tcp", raddr)
		if err != nil {
			return -1, err
		}

		cp.src = conn
		src = conn

		go func() {
			ready <- struct{}{}
		}()
	}

	laddr, err := net.ResolveTCPAddr("tcp", s.ahost+":0")
	if err != nil {
		return -1, err
	}

	lis, err := net.ListenTCP("tcp", laddr)
	if err != nil {
		return -1, err
	}
	cp.dst = lis

	go func() {
		conn, err := lis.Accept()
		if err != nil {
			if s.verbose {
				log.Println("Could not accept dst connection, stopping:", err)
			}

			close(ready)

			return
		}

		dst = conn

		ready <- struct{}{}
	}()

	go func() {
		i := 0
		for range ready {
			i++

			if i == 2 {
				break
			}
		}

		if i < 2 {
			if s.verbose {
				log.Println("Could not accept src or dst connection, stopping")
			}
		}

		go func() {
			if _, err := io.Copy(src, dst); err != nil {
				if s.verbose {
					log.Println("Could not copy from dst to src, stopping:", err)
				}
			}
		}()

		go func() {
			if _, err := io.Copy(dst, src); err != nil {
				if s.verbose {
					log.Println("Could not copy from src to dst, stopping:", err)
				}
			}
		}()
	}()

	s.routesLock.Lock()
	s.routes[routeID] = cp
	s.routesLock.Unlock()

	return laddr.Port, nil
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
