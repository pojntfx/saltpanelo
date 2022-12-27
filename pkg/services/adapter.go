package services

import (
	"context"
	"errors"
	"log"
)

var (
	ErrNoPeersFound = errors.New("could not find any peers")
)

type AdapterRemote struct {
	RequestCall func(ctx context.Context, srcID string) (bool, error)
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

func (a *Adapter) RequestCall(ctx context.Context, srcID string) (bool, error) {
	if a.verbose {
		log.Println("Remote with ID", srcID, "is requesting a call")
	}

	accept, err := a.onRequestCall(ctx, srcID)
	if err != nil {
		return false, err
	}

	return accept, nil
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
