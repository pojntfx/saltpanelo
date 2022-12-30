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

type CallRequestResponse struct {
	Accept      bool
	Latencies   []time.Duration
	Throughputs []ThroughputResult
}

type AdapterRemote struct {
	RequestCall func(
		ctx context.Context,
		srcID string,
		timeout time.Duration,
		addrs []string,
		length,
		chunks int64,
	) (CallRequestResponse, error)
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
	timeout time.Duration,
	addrs []string,
	length,
	chunks int64,
) (CallRequestResponse, error) {
	if a.verbose {
		log.Println("Remote with ID", srcID, "is requesting a call")
	}

	accept, err := a.onRequestCall(ctx, srcID)
	if err != nil {
		return CallRequestResponse{
			Accept:      false,
			Latencies:   []time.Duration{},
			Throughputs: []ThroughputResult{},
		}, err
	}

	if !accept {
		return CallRequestResponse{
			Accept:      false,
			Latencies:   []time.Duration{},
			Throughputs: []ThroughputResult{},
		}, nil
	}

	latencyRes := make(chan []time.Duration)
	throughputRes := make(chan []ThroughputResult)
	errs := make(chan error)

	go func() {
		latency, err := testLatency(timeout, addrs)
		if err != nil {
			errs <- err

			return
		}

		latencyRes <- latency
	}()

	go func() {
		throughput, err := testThroughput(timeout, addrs, length, chunks)
		if err != nil {
			errs <- err

			return
		}

		throughputRes <- throughput
	}()

	var latency []time.Duration
	var throughput []ThroughputResult
l:
	for {
		select {
		case l := <-latencyRes:
			latency = l

			if len(latency) >= len(addrs) && len(throughput) >= len(addrs) {
				break l
			}
		case t := <-throughputRes:
			throughput = t

			if len(latency) >= len(addrs) && len(throughput) >= len(addrs) {
				break l
			}
		case err := <-errs:
			return CallRequestResponse{
				Accept:      false,
				Latencies:   []time.Duration{},
				Throughputs: []ThroughputResult{},
			}, err
		}
	}

	return CallRequestResponse{
		Accept:      true,
		Latencies:   latency,
		Throughputs: throughput,
	}, nil
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
