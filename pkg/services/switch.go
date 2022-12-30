package services

import (
	"context"
	"io"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"
)

type SwitchRemote struct {
	TestLatency    func(ctx context.Context, timeout time.Duration, addrs []string) ([]time.Duration, error)
	TestThroughput func(ctx context.Context, timeout time.Duration, addrs []string, length, chunks int64) ([]ThroughputResult, error)
}

type ThroughputResult struct {
	Read  time.Duration
	Write time.Duration
}

type Switch struct {
	verbose bool

	Peers func() map[string]RouterRemote
}

func NewSwitch(verbose bool) *Switch {
	return &Switch{
		verbose: verbose,
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
