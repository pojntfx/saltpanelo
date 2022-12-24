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
	ErrAdapterAlreadyRegistered = errors.New("could not register adapter: A adapter with this remote ID is already registered")
	ErrDstNotFound              = errors.New("could not find destination")
	ErrDstIsSrc                 = errors.New("could not find route when dst and src are the same")
)

type RouterRemote struct {
	RegisterSwitch  func(ctx context.Context, addr string) error
	RegisterAdapter func(ctx context.Context) error
	FindRoute       func(ctx context.Context, dstID string) ([]string, error)
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

	adaptersLock sync.Mutex
	adapters     map[string]struct{}

	latencyTestInterval time.Duration
	latencyTestTimeout  time.Duration

	throughputLength int64
	throughputChunks int64

	metrics *Metrics

	graphLock sync.Mutex
	graph     graph.Graph[string, string]

	verbose bool

	Peers func() map[string]SwitchRemote
}

func NewRouter(
	verbose bool,

	latencyTestInterval time.Duration,
	latencyTestTimeout time.Duration,

	throughputLength int64,
	throughputChunks int64,

	metrics *Metrics,
) *Router {
	return &Router{
		switches: map[string]switchMetadata{},

		adapters: map[string]struct{}{},

		latencyTestInterval: latencyTestInterval,
		latencyTestTimeout:  latencyTestTimeout,

		throughputLength: throughputLength,
		throughputChunks: throughputChunks,

		metrics: metrics,

		graph: graph.New(graph.StringHash, graph.Directed(), graph.Weighted()),

		verbose: verbose,
	}
}

func (r *Router) updateGraph(ctx context.Context) error {
	r.graphLock.Lock()
	r.switchesLock.Lock()
	r.adaptersLock.Lock()

	r.graph = graph.New(graph.StringHash, graph.Directed(), graph.Weighted())

	for swID := range r.switches {
		if err := r.graph.AddVertex(swID); err != nil {
			return err
		}
	}

	for swID := range r.switches {
		for candidateID := range r.switches {
			// Don't link to self
			if swID == candidateID {
				continue
			}

			// TODO: Also add throughput as weight
			latency, ok := r.switches[swID].latencies[candidateID]
			if !ok {
				continue
			}

			if err := r.graph.AddEdge(swID, candidateID, graph.EdgeWeight(int(latency.Nanoseconds()))); err != nil {
				return err
			}
		}
	}

	for aID := range r.adapters {
		if err := r.graph.AddVertex(aID); err != nil {
			return err
		}
	}

	g := r.graph

	r.switchesLock.Unlock()
	r.adaptersLock.Unlock()
	r.graphLock.Unlock()

	return r.metrics.visualize(ctx, g)
}

func (r *Router) onClientDisconnect(remoteID string) {
	r.switchesLock.Lock()
	r.adaptersLock.Lock()

	delete(r.switches, remoteID)
	delete(r.adapters, remoteID)

	r.adaptersLock.Unlock()
	r.switchesLock.Unlock()

	if r.verbose {
		log.Println("Removed switch or adapter with ID", remoteID, "from topology")
	}

	if err := r.updateGraph(context.Background()); err != nil {
		log.Println("Could not update graph, continuing:", err)
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
			swIDs := []string{}
			for swID, sw := range r.switches {
				// Don't test latency to self
				if swID == remoteID {
					continue
				}

				addrs = append(addrs, sw.addr)
				swIDs = append(swIDs, swID)
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
				for i, swID := range swIDs {
					results[swID] = testResults[i]
				}

				r.switchesLock.Lock()

				sm, ok := r.switches[remoteID]
				if !ok {
					r.switchesLock.Unlock()

					return
				}

				sm.latencies = results

				r.switches[remoteID] = sm

				r.switchesLock.Unlock()

				if r.verbose {
					log.Println("Finished latency tests for switch with ID", remoteID, ":", sm.latencies)
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

				r.switches[remoteID] = sm

				r.switchesLock.Unlock()

				if r.verbose {
					log.Println("Finished throughput tests for switch with ID", remoteID, ":", sm.throughputs)
				}

				if err := r.updateGraph(context.Background()); err != nil {
					log.Println("Could not update graph, continuing:", err)
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
		log.Println("Added switch with ID", remoteID, "to topology")
	}

	return nil
}

func (r *Router) RegisterAdapter(ctx context.Context) error {
	remoteID := rpc.GetRemoteID(ctx)

	r.adaptersLock.Lock()
	defer r.adaptersLock.Unlock()

	if _, ok := r.adapters[remoteID]; ok {
		return ErrAdapterAlreadyRegistered
	}

	r.adapters[remoteID] = struct{}{}

	if r.verbose {
		log.Println("Added adapter with ID", remoteID, "to topology")
	}

	return nil
}

func (r *Router) FindRoute(ctx context.Context, dstID string) ([]string, error) {
	remoteID := rpc.GetRemoteID(ctx)

	r.switchesLock.Lock()
	r.adaptersLock.Lock()
	defer func() {
		r.adaptersLock.Unlock()
		r.switchesLock.Unlock()
	}()

	if _, ok := r.adapters[dstID]; !ok {
		return []string{}, ErrDstNotFound
	}

	if remoteID == dstID {
		return []string{}, ErrDstIsSrc
	}

	if r.verbose {
		log.Println("Finding route from adapter with ID", remoteID, "to adapter with ID", dstID)
	}

	return []string{}, nil
}
