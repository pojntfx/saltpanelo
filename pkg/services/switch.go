package services

import (
	"context"
	"log"
	"net"
	"sync"
	"time"
)

type SwitchRemote struct {
	TestRTT func(ctx context.Context, timeout time.Duration, addrs []string) ([]time.Duration, error)
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

func (s *Switch) TestRTT(ctx context.Context, timeout time.Duration, addrs []string) ([]time.Duration, error) {
	if s.verbose {
		log.Println("Starting RTT tests for addrs", addrs)
	}

	rtts := []time.Duration{}
	var rttLock sync.Mutex

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

			rtt := time.Since(before)

			_ = conn.Close()

			rttLock.Lock()
			rtts = append(rtts, rtt)
			rttLock.Unlock()
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
		return rtts, nil
	}
}
