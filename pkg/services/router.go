package services

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/dominikbraun/graph"
	"github.com/google/uuid"
	"github.com/pojntfx/dudirekta/pkg/rpc"
	"golang.org/x/exp/slices"
)

var (
	ErrSwitchAlreadyRegistered  = errors.New("could not register switch: A switch with this remote ID is already registered")
	ErrAdapterAlreadyRegistered = errors.New("could not register adapter: An adapter with this remote ID is already registered")
	ErrDstNotFound              = errors.New("could not find destination")
	ErrDstIsSrc                 = errors.New("could not find route when dst and src are the same")
	ErrRouteNotFound            = errors.New("could not find route")
)

type RouterRemote struct {
	RegisterSwitch func(ctx context.Context, addr string) error
}

func HandleRouterClientDisconnect(router *Router, remoteID string) error {
	go func() {
		if err := router.unprovisionRouteForSwitch(remoteID); err != nil {
			log.Println("Could not unprovision route for switch with ID", remoteID, ", continuing:", err)
		}
	}()

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

	routesLock sync.Mutex
	routes     map[string][]string

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

		routes: map[string][]string{},

		verbose: verbose,
	}
}

func (r *Router) updateGraphs(ctx context.Context) error {
	r.switchesLock.Lock()

	s := map[string]SwitchMetadata{}
	for k, v := range r.switches {
		s[k] = v
	}

	r.switchesLock.Unlock()

	a := r.Gateway.getAdapters()

	g, err := createNetworkGraph(s, a)
	if err != nil {
		return err
	}

	r.graphLock.Lock()
	r.graph = g
	r.graphLock.Unlock()

	r.routesLock.Lock()

	routes := map[string][]string{}
	for k, v := range r.routes {
		routes[k] = v
	}

	r.routesLock.Unlock()

	go func() {
		if err := r.Metrics.visualize(ctx, s, a, routes); err != nil {
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

	return r.updateGraphs(context.Background())
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

				if err := r.updateGraphs(context.Background()); err != nil {
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

				if err := r.updateGraphs(context.Background()); err != nil {
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

func (r *Router) provisionRoute(srcID, dstID string) (string, error) {
	if r.verbose {
		log.Println("Provisioning route from", srcID, "to", dstID)
	}

	r.graphLock.Lock()

	path, err := graph.ShortestPath(r.graph, srcID, dstID)
	if err != nil {
		r.graphLock.Unlock()

		return "", err
	}

	if len(path) < 3 {
		r.graphLock.Unlock()

		return "", ErrRouteNotFound
	}

	r.graphLock.Unlock()

	routeID := uuid.NewString()

	// TODO: Call `ProvisionRoute` in adapters & switches for route with `routeID`
	// (On switches) It should dial `srcAddr` and create `net.Listener` that is being copied to/from `srcAddr`, then return that port

	r.routesLock.Lock()
	r.routes[routeID] = path
	r.routesLock.Unlock()

	if err := r.updateGraphs(context.Background()); err != nil {
		return "", err
	}

	return routeID, nil
}

// TODO: Add same handler for adapters
func (r *Router) unprovisionRouteForSwitch(swID string) error {
	if r.verbose {
		log.Println("Unprovisioning all routes for switch", swID)
	}

	r.routesLock.Lock()

	switchesToClose := map[string][]SwitchRemote{}
	peers := r.Peers()

	for routeID, route := range r.routes {
		if slices.Contains(route, swID) {
			for _, candidateID := range route {
				if swID == candidateID {
					// Don't call `close` on the switch with `swID` as its already disconnected at this point
					continue
				}

				sw, ok := peers[candidateID]
				if !ok {
					log.Println("Could not find switch with ID", swID, "to close route for, continuing")

					continue
				}

				if _, ok := switchesToClose[routeID]; !ok {
					switchesToClose[routeID] = []SwitchRemote{}
				}

				switchesToClose[routeID] = append(switchesToClose[routeID], sw)
			}

			delete(r.routes, routeID)
		}
	}

	r.routesLock.Unlock()

	var wg sync.WaitGroup

	for routeID, switches := range switchesToClose {
		for _, sw := range switches {
			wg.Add(1)

			go func(routeID string, sw SwitchRemote) {
				defer wg.Done()

				if err := sw.UnprovisionRoute(context.Background(), routeID); err != nil {
					log.Println("Could not unprovision route", routeID, "for switch with ID", swID, ", continuing:", err)
				}
			}(routeID, sw)
		}
	}

	wg.Wait()

	if err := r.updateGraphs(context.Background()); err != nil {
		return err
	}

	return nil
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

	return r.updateGraphs(context.Background())
}
