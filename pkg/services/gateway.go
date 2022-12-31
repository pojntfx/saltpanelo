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
	ErrInvalidLatencyTestResultLength    = errors.New("received invalid length of latency test results")
	ErrInvalidThroughputTestResultLength = errors.New("received invalid length of throughput test results")
)

type GatewayRemote struct {
	RegisterAdapter func(ctx context.Context) error
	RequestCall     func(ctx context.Context, dstID string) (bool, error)
}

func HandleGatewayClientDisconnect(gateway *Gateway, remoteID string) error {
	return gateway.onClientDisconnect(remoteID)
}

type AdapterMetadata struct {
	Latencies   map[string]time.Duration
	Throughputs map[string]ThroughputResult
}

type Gateway struct {
	verbose bool

	adaptersLock sync.Mutex
	adapters     map[string]AdapterMetadata

	Router *Router

	Peers func() map[string]AdapterRemote
}

func NewGateway(
	verbose bool,
) *Gateway {
	return &Gateway{
		verbose: verbose,

		adapters: map[string]AdapterMetadata{},
	}
}

func (g *Gateway) onClientDisconnect(remoteID string) error {
	g.adaptersLock.Lock()

	delete(g.adapters, remoteID)

	g.adaptersLock.Unlock()

	if g.verbose {
		log.Println("Removed adapter with ID", remoteID, "from topology")
	}

	return g.Router.updateGraph(context.Background())
}

func (g *Gateway) getAdapters() map[string]AdapterMetadata {
	g.adaptersLock.Lock()
	defer g.adaptersLock.Unlock()

	a := map[string]AdapterMetadata{}
	for k, v := range g.adapters {
		a[k] = v
	}

	return a
}

func (g *Gateway) RegisterAdapter(ctx context.Context) error {
	remoteID := rpc.GetRemoteID(ctx)

	g.adaptersLock.Lock()

	if _, ok := g.adapters[remoteID]; ok {
		g.adaptersLock.Unlock()

		return ErrAdapterAlreadyRegistered
	}

	g.adapters[remoteID] = AdapterMetadata{
		map[string]time.Duration{},
		map[string]ThroughputResult{},
	}

	if g.verbose {
		log.Println("Added adapter with ID", remoteID, "to topology")
	}

	g.adaptersLock.Unlock()

	return g.Router.updateGraph(context.Background())
}

func (g *Gateway) RequestCall(ctx context.Context, dstID string) (bool, error) {
	remoteID := rpc.GetRemoteID(ctx)

	g.adaptersLock.Lock()

	if g.verbose {
		log.Println("Remote with ID", remoteID, "is requesting a call with ID", dstID)
	}

	if _, ok := g.adapters[dstID]; !ok {
		g.adaptersLock.Unlock()

		return false, ErrDstNotFound
	}

	if remoteID == dstID {
		g.adaptersLock.Unlock()

		return false, ErrDstIsSrc
	}

	g.adaptersLock.Unlock()

	for candidateID, peer := range g.Peers() {
		if dstID == candidateID {
			g.adaptersLock.Lock()

			addrs := []string{}
			swIDs := []string{}
			for swID, sw := range g.Router.getSwitches() {
				addrs = append(addrs, sw.Addr)
				swIDs = append(swIDs, swID)
			}

			g.adaptersLock.Unlock()

			callRequestResponse, err := peer.RequestCall(
				ctx,
				remoteID,
				g.Router.testTimeout,
				addrs,
				g.Router.throughputLength,
				g.Router.throughputChunks,
			)
			if err != nil {
				return false, err
			}

			if !callRequestResponse.Accept {
				return false, nil
			}

			if len(callRequestResponse.Latencies) < len(addrs) {
				log.Printf("%v: for ID %v, stopping", ErrInvalidLatencyTestResultLength, candidateID)

				return false, ErrInvalidLatencyTestResultLength
			}

			if len(callRequestResponse.Throughputs) < len(addrs) {
				log.Printf("%v: for ID %v, stopping", ErrInvalidThroughputTestResultLength, candidateID)

				return false, ErrInvalidThroughputTestResultLength
			}

			latencies := map[string]time.Duration{}
			for i, swID := range swIDs {
				latencies[swID] = callRequestResponse.Latencies[i]
			}

			throughputs := map[string]ThroughputResult{}
			for i, swID := range swIDs {
				throughputs[swID] = callRequestResponse.Throughputs[i]
			}

			g.adaptersLock.Lock()

			sm, ok := g.adapters[remoteID]
			if !ok {
				g.adaptersLock.Unlock()

				break
			}

			sm.Latencies = latencies
			sm.Throughputs = throughputs

			g.adapters[remoteID] = sm

			g.adaptersLock.Unlock()

			if g.verbose {
				log.Println("Finished requesting call for ID", candidateID)
			}

			if err := g.Router.updateGraph(context.Background()); err != nil {
				return false, err
			}

			return callRequestResponse.Accept, nil
		}
	}

	return false, ErrDstNotFound
}
