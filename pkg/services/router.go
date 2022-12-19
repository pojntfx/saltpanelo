package services

import (
	"context"
	"errors"
	"log"
	"sync"

	"github.com/pojntfx/dudirekta/pkg/rpc"
)

var (
	ErrSwitchAlreadyRegistered = errors.New("could not register switch: A switch with this peer ID is already registered")
)

type RouterRemote struct{}

func HandleClientDisconnect(router *Router, remoteID string) {
	router.onClientDisconnect(remoteID)
}

type Router struct {
	switchesLock sync.Mutex
	switches     map[string]string // Maps peer ID to addr

	verbose bool

	Peers func() map[string]SwitchRemote
}

func NewRouter(verbose bool) *Router {
	return &Router{
		switches: map[string]string{},

		verbose: verbose,
	}
}

func (r *Router) onClientDisconnect(remoteID string) {
	r.switchesLock.Lock()
	defer r.switchesLock.Unlock()

	delete(r.switches, remoteID)

	if r.verbose {
		log.Println("Removed switch with ID", remoteID, "from topology")
	}
}

func (r *Router) RegisterSwitch(ctx context.Context, addr string) (string, error) {
	remoteID := rpc.GetRemoteID(ctx)

	r.switchesLock.Lock()
	defer r.switchesLock.Unlock()

	if _, ok := r.switches[remoteID]; ok {
		return "", ErrSwitchAlreadyRegistered
	}

	r.switches[remoteID] = addr

	if r.verbose {
		log.Println("Added switch with ID", remoteID, "from topology")
	}

	return remoteID, nil
}
