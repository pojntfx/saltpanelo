package services

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/dominikbraun/graph"
	"github.com/pojntfx/dudirekta/pkg/rpc"
)

var (
	ErrSwitchAlreadyRegistered  = errors.New("could not register switch: A switch with this remote ID is already registered")
	ErrAdapterAlreadyRegistered = errors.New("could not register adapter: An adapter with this remote ID is already registered")
	ErrDstNotFound              = errors.New("could not find destination")
	ErrDstIsSrc                 = errors.New("could not find route when dst and src are the same")
)

type RouterRemote struct {
	RegisterSwitch func(ctx context.Context, addr string) error
}

func HandleRouterClientDisconnect(router *Router, remoteID string) error {
	return router.onClientDisconnect(remoteID)
}

func HandleRouterOpen(router *Router) {
	router.onOpen()
}

type SwitchMetadata struct {
	Addr        string
	Latencies   map[string]time.Duration
	Throughputs map[string]ThroughputResult
}

type Router struct {
	switchesLock sync.Mutex
	switches     map[string]SwitchMetadata

	testInterval time.Duration
	testTimeout  time.Duration

	throughputLength int64
	throughputChunks int64

	Metrics *Metrics
	Gateway *Gateway

	graphLock sync.Mutex
	graph     graph.Graph[string, string]

	verbose bool

	Peers func() map[string]SwitchRemote
}

func NewRouter(
	verbose bool,

	testInterval time.Duration,
	testTimeout time.Duration,

	throughputLength int64,
	throughputChunks int64,
) *Router {
	return &Router{
		switches: map[string]SwitchMetadata{},

		testInterval: testInterval,
		testTimeout:  testTimeout,

		throughputLength: throughputLength,
		throughputChunks: throughputChunks,

		graph: graph.New(graph.StringHash, graph.Directed(), graph.Weighted()),

		verbose: verbose,
	}
}

func (r *Router) updateGraph(ctx context.Context) error {
	r.switchesLock.Lock()

	s := map[string]SwitchMetadata{}
	for k, v := range r.switches {
		s[k] = v
	}

	r.switchesLock.Unlock()

	a := r.Gateway.getAdapters()

	g, err := createGraph(s, a)
	if err != nil {
		return err
	}

	r.graphLock.Lock()
	r.graph = g
	r.graphLock.Unlock()

	go func() {
		if err := r.Metrics.visualize(ctx, s, a); err != nil {
			log.Println("Could visualize graph, continuing:", err)
		}
	}()

	return nil
}

func (r *Router) onClientDisconnect(remoteID string) error {
	r.switchesLock.Lock()

	delete(r.switches, remoteID)

	r.switchesLock.Unlock()

	if r.verbose {
		log.Println("Removed switch with ID", remoteID, "from topology")
	}

	return r.updateGraph(context.Background())
}

func (r *Router) onOpen() {
	t := time.NewTicker(r.testInterval)
	defer t.Stop()

	for range t.C {
		var wg sync.WaitGroup

		for remoteID, peer := range r.Peers() {
			wg.Add(2)

			r.switchesLock.Lock()
			addrs := []string{}
			swIDs := []string{}
			for swID, sw := range r.switches {
				// Don't test latency to self
				if swID == remoteID {
					continue
				}

				addrs = append(addrs, sw.Addr)
				swIDs = append(swIDs, swID)
			}
			r.switchesLock.Unlock()

			go func(remoteID string, peer SwitchRemote) {
				defer wg.Done()

				if r.verbose {
					log.Println("Starting latency tests for switch with ID", remoteID)
				}

				testResults, err := peer.TestLatency(nil, r.testTimeout, addrs)
				if err != nil {
					log.Println("Could not run latency test for switch with ID", remoteID, ", continuing:", err)

					return
				}

				if len(testResults) < len(addrs) {
					log.Printf("%v: for ID %v, continuing", ErrInvalidLatencyTestResultLength, remoteID)

					return
				}

				results := map[string]time.Duration{}
				for i, swID := range swIDs {
					results[swID] = testResults[i]
				}

				r.switchesLock.Lock()

				sm, ok := r.switches[remoteID]
				if !ok {
					r.switchesLock.Unlock()

					return
				}

				sm.Latencies = results

				r.switches[remoteID] = sm

				r.switchesLock.Unlock()

				if r.verbose {
					log.Println("Finished latency tests for switch with ID", remoteID, ":", sm.Latencies)
				}

				if err := r.updateGraph(context.Background()); err != nil {
					log.Println("Could not update graph, continuing:", err)
				}
			}(remoteID, peer)

			go func(remoteID string, peer SwitchRemote) {
				defer wg.Done()

				if r.verbose {
					log.Println("Starting throughput tests for switch with ID", remoteID)
				}

				testResults, err := peer.TestThroughput(nil, r.testTimeout, addrs, r.throughputLength, r.throughputChunks)
				if err != nil {
					log.Println("Could not run throughput test for switch with ID", remoteID, ", continuing:", err)

					return
				}

				if len(testResults) < len(addrs) {
					log.Printf("%v: for ID %v, continuing", ErrInvalidThroughputTestResultLength, remoteID)

					return
				}

				results := map[string]ThroughputResult{}
				for i, swID := range swIDs {
					results[swID] = testResults[i]
				}

				r.switchesLock.Lock()

				sm, ok := r.switches[remoteID]
				if !ok {
					r.switchesLock.Unlock()

					return
				}

				sm.Throughputs = results

				r.switches[remoteID] = sm

				r.switchesLock.Unlock()

				if r.verbose {
					log.Println("Finished throughput tests for switch with ID", remoteID, ":", sm.Throughputs)
				}

				if err := r.updateGraph(context.Background()); err != nil {
					log.Println("Could not update graph, continuing:", err)
				}
			}(remoteID, peer)
		}

		wg.Wait()
	}
}

func (r *Router) getSwitches() map[string]SwitchMetadata {
	r.switchesLock.Lock()
	defer r.switchesLock.Unlock()

	a := map[string]SwitchMetadata{}
	for k, v := range r.switches {
		a[k] = v
	}

	return a
}

func (r *Router) RegisterSwitch(ctx context.Context, addr string) error {
	remoteID := rpc.GetRemoteID(ctx)

	r.switchesLock.Lock()

	if _, ok := r.switches[remoteID]; ok {
		r.switchesLock.Unlock()

		return ErrSwitchAlreadyRegistered
	}

	r.switches[remoteID] = SwitchMetadata{
		addr,
		map[string]time.Duration{},
		map[string]ThroughputResult{},
	}

	if r.verbose {
		log.Println("Added switch with ID", remoteID, "to topology")
	}

	r.switchesLock.Unlock()

	return r.updateGraph(context.Background())
}
