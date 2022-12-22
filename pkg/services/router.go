package services

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/pojntfx/dudirekta/pkg/rpc"
)

var (
	ErrSwitchAlreadyRegistered = errors.New("could not register switch: A switch with this peer ID is already registered")
)

type RouterRemote struct {
	RegisterSwitch func(ctx context.Context, addr string) error
}

func HandleClientDisconnect(router *Router, remoteID string) {
	router.onClientDisconnect(remoteID)
}

func HandleOpen(router *Router) {
	router.onOpen()
}

type switchMetadata struct {
	addr        string
	latencies   map[string]time.Duration
	throughputs map[string]ThroughputResult
}

type Router struct {
	switchesLock sync.Mutex
	switches     map[string]switchMetadata

	latencyTestInterval time.Duration
	latencyTestTimeout  time.Duration

	throughputLength int64
	throughputChunks int64

	verbose bool

	Peers func() map[string]SwitchRemote
}

func NewRouter(
	verbose bool,

	latencyTestInterval time.Duration,
	latencyTestTimeout time.Duration,

	throughputLength int64,
	throughputChunks int64,
) *Router {
	return &Router{
		switches: map[string]switchMetadata{},

		latencyTestInterval: latencyTestInterval,
		latencyTestTimeout:  latencyTestTimeout,

		throughputLength: throughputLength,
		throughputChunks: throughputChunks,

		verbose: verbose,
	}
}

func (r *Router) onClientDisconnect(remoteID string) {
	r.switchesLock.Lock()
	defer r.switchesLock.Unlock()

	delete(r.switches, remoteID)

	if r.verbose {
		log.Println("Removed switch with ID", remoteID, "from topology")
	}
}

func (r *Router) onOpen() {
	t := time.NewTicker(r.latencyTestInterval)
	defer t.Stop()

	for range t.C {
		var wg sync.WaitGroup

		for remoteID, peer := range r.Peers() {
			wg.Add(2)

			r.switchesLock.Lock()
			addrs := []string{}
			for id, sw := range r.switches {
				// Don't test latency to self
				if id == remoteID {
					continue
				}

				addrs = append(addrs, sw.addr)
			}
			r.switchesLock.Unlock()

			go func(remoteID string, peer SwitchRemote) {
				defer wg.Done()

				if r.verbose {
					log.Println("Starting latency tests for switch with ID", remoteID)
				}

				testResults, err := peer.TestLatency(nil, r.latencyTestTimeout, addrs)
				if err != nil {
					log.Println("Could not run latency test for switch with ID", remoteID, ", continuing:", err)

					return
				}

				if len(testResults) < len(addrs) {
					log.Println("Received invalid length of latency test results from switch with ID", remoteID, ", continuing")

					return
				}

				results := map[string]time.Duration{}
				for i, addr := range addrs {
					results[addr] = testResults[i]
				}

				r.switchesLock.Lock()

				sm, ok := r.switches[remoteID]
				if !ok {
					r.switchesLock.Unlock()

					return
				}

				sm.latencies = results

				r.switchesLock.Unlock()

				if r.verbose {
					log.Println("Finished latency tests for switch with ID", remoteID, ":", sm.latencies)
				}
			}(remoteID, peer)

			go func(remoteID string, peer SwitchRemote) {
				defer wg.Done()

				if r.verbose {
					log.Println("Starting throughput tests for switch with ID", remoteID)
				}

				testResults, err := peer.TestThroughput(nil, r.latencyTestTimeout, addrs, r.throughputLength, r.throughputChunks)
				if err != nil {
					log.Println("Could not run throughput test for switch with ID", remoteID, ", continuing:", err)

					return
				}

				if len(testResults) < len(addrs) {
					log.Println("Received invalid length of throughput test results from switch with ID", remoteID, ", continuing")

					return
				}

				results := map[string]ThroughputResult{}
				for i, addr := range addrs {
					results[addr] = testResults[i]
				}

				r.switchesLock.Lock()

				sm, ok := r.switches[remoteID]
				if !ok {
					r.switchesLock.Unlock()

					return
				}

				sm.throughputs = results

				r.switchesLock.Unlock()

				if r.verbose {
					log.Println("Finished throughput tests for switch with ID", remoteID, ":", sm.throughputs)
				}
			}(remoteID, peer)
		}

		wg.Wait()
	}
}

func (r *Router) RegisterSwitch(ctx context.Context, addr string) error {
	remoteID := rpc.GetRemoteID(ctx)

	r.switchesLock.Lock()
	defer r.switchesLock.Unlock()

	if _, ok := r.switches[remoteID]; ok {
		return ErrSwitchAlreadyRegistered
	}

	r.switches[remoteID] = switchMetadata{
		addr,
		map[string]time.Duration{},
		map[string]ThroughputResult{},
	}

	if r.verbose {
		log.Println("Added switch with ID", remoteID, "from topology")
	}

	return nil
}
