package services

import (
	"context"
	"errors"
	"log"
	"time"
)

var (
	ErrNoPeersFound = errors.New("could not find any peers")
)

type AdapterRemote struct {
	RequestCall    func(ctx context.Context, srcID string) (bool, error)
	TestLatency    func(ctx context.Context, timeout time.Duration, addrs []string) ([]time.Duration, error)
	TestThroughput func(ctx context.Context, timeout time.Duration, addrs []string, length, chunks int64) ([]ThroughputResult, error)
}

func RequestCall(adapter *Adapter, dstID string) (bool, error) {
	return adapter.requestCall(context.Background(), dstID)
}

type Adapter struct {
	verbose bool

	onRequestCall func(ctx context.Context, srcID string) (bool, error)

	Peers func() map[string]GatewayRemote
}

func NewAdapter(
	verbose bool,

	onRequestCall func(ctx context.Context, srcID string) (bool, error),
) *Adapter {
	return &Adapter{
		verbose: verbose,

		onRequestCall: onRequestCall,
	}
}

func (a *Adapter) RequestCall(
	ctx context.Context,
	srcID string,
) (bool, error) {
	if a.verbose {
		log.Println("Remote with ID", srcID, "is requesting a call")
	}

	return a.onRequestCall(ctx, srcID)
}

func (a *Adapter) requestCall(
	ctx context.Context,
	dstID string,
) (bool, error) {
	if a.verbose {
		log.Println("Requesting a call with ID", dstID)
	}

	for _, peer := range a.Peers() {
		return peer.RequestCall(ctx, dstID)
	}

	return false, ErrNoPeersFound
}

func (a *Adapter) TestLatency(ctx context.Context, timeout time.Duration, addrs []string) ([]time.Duration, error) {
	if a.verbose {
		log.Println("Starting latency tests for addrs", addrs)
	}

	return testLatency(timeout, addrs)
}

func (a *Adapter) TestThroughput(ctx context.Context, timeout time.Duration, addrs []string, length, chunks int64) ([]ThroughputResult, error) {
	if a.verbose {
		log.Println("Starting throughput tests for addrs", addrs)
	}

	return testThroughput(timeout, addrs, length, chunks)
}
