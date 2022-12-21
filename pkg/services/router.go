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
	RegisterSwitch func(ctx context.Context, addr string) error
}

func HandleClientDisconnect(router *Router, remoteID string) {
	router.onClientDisconnect(remoteID)
}

func HandleOpen(router *Router) {
	router.onOpen()
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

func (r *Router) onOpen() {
	t := time.NewTicker(r.rttTestInterval)
	defer t.Stop()

	for range t.C {
		var wg sync.WaitGroup

		for remoteID, peer := range r.Peers() {
			wg.Add(1)

			go func(remoteID string, peer SwitchRemote) {
				defer wg.Done()

				r.switchesLock.Lock()
				addrs := []string{}
				for id, sw := range r.switches {
					// Don't test RTT to self
					if id == remoteID {
						continue
					}

					addrs = append(addrs, sw.addr)
				}
				r.switchesLock.Unlock()

				if r.verbose {
					log.Println("Starting RTT tests for switch with ID", remoteID)
				}

				rttTestResults, err := peer.TestRTT(nil, r.rttTestTimeout, addrs)
				if err != nil {
					log.Println("Could not run RTT test for switch with ID", remoteID, ", continuing:", err)

					return
				}

				if len(rttTestResults) < len(addrs) {
					log.Println("Received invalid length of RTT tests results from switch with ID", remoteID, ", continuing")

					return
				}

				rtts := map[string]time.Duration{}
				for i, addr := range addrs {
					rtts[addr] = rttTestResults[i]
				}

				r.switchesLock.Lock()

				sm, ok := r.switches[remoteID]
				if !ok {
					r.switchesLock.Unlock()

					return
				}

				sm.rtts = rtts

				r.switchesLock.Unlock()

				if r.verbose {
					log.Println("Finished RTT tests for switch with ID", remoteID, ":", sm.rtts)
				}
			}(remoteID, peer)
		}

		wg.Wait()
	}
}

func (r *Router) RegisterSwitch(ctx context.Context, addr string) error {
	remoteID := rpc.GetRemoteID(ctx)

	r.switchesLock.Lock()
	defer r.switchesLock.Unlock()

	if _, ok := r.switches[remoteID]; ok {
		return ErrSwitchAlreadyRegistered
	}

	r.switches[remoteID] = switchMetadata{
		addr,
		map[string]time.Duration{},
	}

	if r.verbose {
		log.Println("Added switch with ID", remoteID, "from topology")
	}

	return nil
}
