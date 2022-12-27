package services

import (
	"context"
	"log"
	"sync"

	"github.com/pojntfx/dudirekta/pkg/rpc"
)

type GatewayRemote struct {
	RegisterAdapter func(ctx context.Context) error
	RequestCall     func(ctx context.Context, dstID string) (bool, error)
}

func HandleGatewayClientDisconnect(gateway *Gateway, remoteID string) {
	gateway.onClientDisconnect(remoteID)
}

type Gateway struct {
	verbose bool

	adaptersLock sync.Mutex
	adapters     map[string]struct{}

	Router *Router

	Peers func() map[string]AdapterRemote
}

func NewGateway(
	verbose bool,
) *Gateway {
	return &Gateway{
		verbose: verbose,

		adapters: map[string]struct{}{},
	}
}

func (g *Gateway) onClientDisconnect(remoteID string) {
	g.adaptersLock.Lock()

	delete(g.adapters, remoteID)

	g.adaptersLock.Unlock()

	if g.verbose {
		log.Println("Removed adapter with ID", remoteID, "from topology")
	}

	if err := g.Router.updateGraph(context.Background()); err != nil {
		log.Println("Could not update graph, continuing:", err)
	}
}

func (g *Gateway) getAdapters() map[string]struct{} {
	g.adaptersLock.Lock()
	defer g.adaptersLock.Unlock()

	a := map[string]struct{}{}
	for k, v := range g.adapters {
		a[k] = v
	}

	return a
}

func (g *Gateway) RegisterAdapter(ctx context.Context) error {
	remoteID := rpc.GetRemoteID(ctx)

	g.adaptersLock.Lock()
	defer g.adaptersLock.Unlock()

	if _, ok := g.adapters[remoteID]; ok {
		return ErrAdapterAlreadyRegistered
	}

	g.adapters[remoteID] = struct{}{}

	if g.verbose {
		log.Println("Added adapter with ID", remoteID, "to topology")
	}

	return nil
}

func (g *Gateway) RequestCall(ctx context.Context, dstID string) (bool, error) {
	remoteID := rpc.GetRemoteID(ctx)

	if g.verbose {
		log.Println("Remote with ID", remoteID, "is requesting a call with ID", dstID)
	}

	if _, ok := g.adapters[dstID]; !ok {
		return false, ErrDstNotFound
	}

	if remoteID == dstID {
		return false, ErrDstIsSrc
	}

	for _, peer := range g.Peers() {
		if remoteID == dstID {
			return peer.RequestCall(ctx, remoteID)
		}
	}

	return false, ErrDstNotFound
}
