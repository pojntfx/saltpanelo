package services

import (
	"context"
	"log"
	"sync"

	"github.com/pojntfx/dudirekta/pkg/rpc"
)

type GatewayRemote struct {
	RegisterAdapter func(ctx context.Context) error
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

	return g.adapters
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
