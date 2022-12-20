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
	ErrSwitchAlreadyRegistered = errors.New("could not register switch: A switch with this peer ID is already registered")
)

type RouterRemote struct {
	RegisterSwitch func(ctx context.Context, addr string) (string, error)
}

func HandleClientDisconnect(router *Router, remoteID string) {
	router.onClientDisconnect(remoteID)
}

type switchMetadata struct {
	addr string
	rtts map[string]time.Duration
}

type Router struct {
	switchesLock sync.Mutex
	switches     map[string]switchMetadata

	rttTestInterval time.Duration
	rttTestTimeout  time.Duration

	verbose bool

	Peers func() map[string]SwitchRemote
}

func NewRouter(
	verbose bool,

	rttTestInterval time.Duration,
	rttTestTimeout time.Duration,
) *Router {
	return &Router{
		switches: map[string]switchMetadata{},

		rttTestInterval: rttTestInterval,
		rttTestTimeout:  rttTestTimeout,

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

	r.switches[remoteID] = switchMetadata{
		addr,
		map[string]time.Duration{},
	}

	if r.verbose {
		log.Println("Added switch with ID", remoteID, "from topology")
	}

	go func() {
		t := time.NewTicker(r.rttTestInterval)
		defer t.Stop()

		for range t.C {
			found := false

			r.switchesLock.Lock()
			addrs := []string{}
			for addr := range r.switches {
				addrs = append(addrs, addr)
			}
			r.switchesLock.Unlock()

			for remoteID, peer := range r.Peers() {
				if remoteID == rpc.GetRemoteID(ctx) {
					found = true

					rttTestResults, err := peer.TestRTT(ctx, r.rttTestTimeout, addrs)
					if err != nil {
						log.Println("Could not run RTT test for switch with ID", remoteID, ", continuing:", err)

						break
					}

					if len(rttTestResults) < len(addrs) {
						log.Println("Received invalid length of RTT tests results from switch with ID", remoteID, ", continuing")

						break
					}

					rtts := map[string]time.Duration{}
					for i, addr := range addrs {
						rtts[addr] = rttTestResults[i]
					}

					r.switchesLock.Lock()

					sm, ok := r.switches[remoteID]
					if !ok {
						found = false

						r.switchesLock.Unlock()

						break
					}

					sm.rtts = rtts

					r.switchesLock.Unlock()

					break
				}
			}

			if !found && r.verbose {
				log.Println("Stopping RTT tests for switch with ID", remoteID, "due to disconnect")

				return
			}
		}
	}()

	return remoteID, nil
}
