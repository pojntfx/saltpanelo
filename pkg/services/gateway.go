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
	ErrSrcNotFound                       = errors.New("could not find source")
	ErrAdapterNotFound                   = errors.New("could not find adapter")
)

type GatewayRemote struct {
	RegisterAdapter func(ctx context.Context) error
	RequestCall     func(ctx context.Context, dstID string) (RequestCallResult, error)
}

type RequestCallResult struct {
	Accept  bool
	RouteID string
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

func (g *Gateway) refreshPeerLatency(
	ctx context.Context,

	remote AdapterRemote,
	remoteID string,

	addrs []string,
	swIDs []string,
) error {
	rawLatencies, err := remote.TestLatency(ctx, g.Router.testTimeout, addrs)
	if err != nil {
		return err
	}

	rawThroughputs, err := remote.TestThroughput(
		ctx,
		g.Router.testTimeout,
		addrs,
		g.Router.throughputLength,
		g.Router.throughputChunks)
	if err != nil {
		return err
	}

	if len(rawLatencies) < len(addrs) {
		log.Printf("%v: for ID %v, stopping", ErrInvalidLatencyTestResultLength, remoteID)

		return ErrInvalidLatencyTestResultLength
	}

	if len(rawThroughputs) < len(addrs) {
		log.Printf("%v: for ID %v, stopping", ErrInvalidThroughputTestResultLength, remoteID)

		return ErrInvalidThroughputTestResultLength
	}

	latencies := map[string]time.Duration{}
	for i, swID := range swIDs {
		latencies[swID] = rawLatencies[i]
	}

	throughputs := map[string]ThroughputResult{}
	for i, swID := range swIDs {
		throughputs[swID] = rawThroughputs[i]
	}

	g.adaptersLock.Lock()

	sm, ok := g.adapters[remoteID]
	if !ok {
		g.adaptersLock.Unlock()

		return ErrAdapterNotFound
	}

	sm.Latencies = latencies
	sm.Throughputs = throughputs

	g.adapters[remoteID] = sm

	g.adaptersLock.Unlock()

	return nil
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

func (g *Gateway) RequestCall(ctx context.Context, dstID string) (RequestCallResult, error) {
	remoteID := rpc.GetRemoteID(ctx)

	g.adaptersLock.Lock()

	if g.verbose {
		log.Println("Remote with ID", remoteID, "is requesting a call with ID", dstID)
	}

	if _, ok := g.adapters[dstID]; !ok {
		g.adaptersLock.Unlock()

		return RequestCallResult{}, ErrDstNotFound
	}

	if remoteID == dstID {
		g.adaptersLock.Unlock()

		return RequestCallResult{}, ErrDstIsSrc
	}

	g.adaptersLock.Unlock()
	var dst *AdapterRemote
	for candidateID, callee := range g.Peers() {
		if dstID == candidateID {
			dst = &callee

			break
		}
	}
	g.adaptersLock.Lock()

	if dst == nil {
		return RequestCallResult{}, ErrDstNotFound
	}

	addrs := []string{}
	swIDs := []string{}
	for swID, sw := range g.Router.getSwitches() {
		addrs = append(addrs, sw.Addr)
		swIDs = append(swIDs, swID)
	}

	g.adaptersLock.Unlock()

	accept, err := dst.RequestCall(
		ctx,
		remoteID,
	)
	if err != nil {
		return RequestCallResult{}, err
	}

	if !accept {
		return RequestCallResult{}, nil
	}

	var caller AdapterRemote
	found := false
	for candidateID, candidatePeer := range g.Peers() {
		if remoteID == candidateID {
			caller = candidatePeer

			found = true

			break
		}
	}

	if !found {
		return RequestCallResult{}, ErrSrcNotFound
	}

	if err := g.refreshPeerLatency(
		ctx,

		*dst,
		dstID,

		addrs,
		swIDs,
	); err != nil {
		return RequestCallResult{}, err
	}

	if err := g.refreshPeerLatency(
		ctx,

		caller,
		remoteID,

		addrs,
		swIDs,
	); err != nil {
		return RequestCallResult{}, err
	}

	if g.verbose {
		log.Println("Finished requesting call for ID", dstID)
	}

	if err := g.Router.updateGraph(context.Background()); err != nil {
		return RequestCallResult{}, err
	}

	routeID, err := g.Router.provisionRoute(remoteID, dstID)
	if err != nil {
		return RequestCallResult{}, err
	}

	return RequestCallResult{
		Accept:  true,
		RouteID: routeID,
	}, nil
}
