package services

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"log"
	"net"
	"sync"
	"time"

	"github.com/dominikbraun/graph"
	"github.com/pojntfx/dudirekta/pkg/rpc"
	"github.com/pojntfx/saltpanelo/pkg/auth"
	"github.com/pojntfx/saltpanelo/pkg/utils"
	"golang.org/x/exp/slices"
)

var (
	ErrSwitchAlreadyRegistered  = errors.New("could not register switch: A switch with this remote ID is already registered")
	ErrAdapterAlreadyRegistered = errors.New("could not register adapter: An adapter with this remote ID is already registered")
	ErrDstNotFound              = errors.New("could not find destination")
	ErrDstIsSrc                 = errors.New("could not find route when dst and src are the same")
	ErrRouteNotFound            = errors.New("could not find route")
	ErrSwitchNotFound           = errors.New("could not find switch")
	ErrInvalidPortsCount        = errors.New("could not proceed with invalid ports count")
)

type RouterRemote struct {
	RegisterSwitch func(ctx context.Context, token string, addr string) (SwitchConfiguration, error)
}

func HandleRouterClientDisconnect(r *Router, g *Gateway, remoteID string) error {
	go func() {
		if err := unprovisionRouteForPeer(r, g, remoteID); err != nil {
			log.Println("Could not unprovision route for switch with ID", remoteID, ", continuing:", err)
		}
	}()

	return r.onClientDisconnect(remoteID)
}

func HandleRouterOpen(router *Router) {
	router.onOpen()
}

type SwitchMetadata struct {
	Addr        string
	Latencies   map[string]time.Duration
	Throughputs map[string]ThroughputResult
}

type CertPair struct {
	CertPEM        []byte
	CertPrivKeyPEM []byte
}

type SwitchConfiguration struct {
	CAPEM               []byte
	BenchmarkListenCert CertPair
}

type Router struct {
	switchesLock sync.Mutex
	switches     map[string]SwitchMetadata

	testInterval time.Duration
	testTimeout  time.Duration

	throughputLength int64
	throughputChunks int64

	Metrics *Metrics
	Gateway *Gateway

	graphLock sync.Mutex
	graph     graph.Graph[string, string]

	routesLock sync.Mutex
	routes     map[string][]string

	verbose bool

	auth *auth.JWTAuthn

	caCfg     *x509.Certificate
	caPEM     []byte
	caPrivKey *rsa.PrivateKey

	callCertValidity,
	benchmarkListenCertValidity,
	benchmarkClientCertValidity time.Duration

	Peers func() map[string]SwitchRemote
}

func NewRouter(
	verbose bool,

	testInterval time.Duration,
	testTimeout time.Duration,

	throughputLength int64,
	throughputChunks int64,

	oidcIssuer,
	oidcClientID,
	oidcAudience string,

	caCfg *x509.Certificate,
	caPEM []byte,
	caPrivKey *rsa.PrivateKey,

	callCertValidity time.Duration,
	benchmarkListenCertValidity time.Duration,
	benchmarkClientCertValidity time.Duration,
) *Router {
	return &Router{
		switches: map[string]SwitchMetadata{},

		testInterval: testInterval,
		testTimeout:  testTimeout,

		throughputLength: throughputLength,
		throughputChunks: throughputChunks,

		graph: graph.New(graph.StringHash, graph.Directed(), graph.Weighted()),

		routes: map[string][]string{},

		verbose: verbose,

		auth: auth.NewJWTAuthn(oidcIssuer, oidcClientID, oidcAudience),

		caCfg:     caCfg,
		caPEM:     caPEM,
		caPrivKey: caPrivKey,

		callCertValidity:            callCertValidity,
		benchmarkListenCertValidity: benchmarkListenCertValidity,
		benchmarkClientCertValidity: benchmarkClientCertValidity,
	}
}

func (r *Router) Open(ctx context.Context) error {
	return r.auth.Open(ctx)
}

func (r *Router) updateGraphs(ctx context.Context) error {
	r.switchesLock.Lock()

	s := map[string]SwitchMetadata{}
	for k, v := range r.switches {
		s[k] = v
	}

	r.switchesLock.Unlock()

	a := r.Gateway.getAdapters()

	g, err := createNetworkGraph(s, a)
	if err != nil {
		return err
	}

	r.graphLock.Lock()
	r.graph = g
	r.graphLock.Unlock()

	r.routesLock.Lock()

	routes := map[string][]string{}
	for k, v := range r.routes {
		routes[k] = v
	}

	r.routesLock.Unlock()

	go func() {
		if err := r.Metrics.visualize(ctx, s, a, routes); err != nil {
			log.Println("Could visualize graph, continuing:", err)
		}
	}()

	return nil
}

func (r *Router) onClientDisconnect(remoteID string) error {
	r.switchesLock.Lock()

	delete(r.switches, remoteID)

	r.switchesLock.Unlock()

	if r.verbose {
		log.Println("Removed switch with ID", remoteID, "from topology")
	}

	return r.updateGraphs(context.Background())
}

func (r *Router) onOpen() {
	t := time.NewTicker(r.testInterval)
	defer t.Stop()

	for range t.C {
		var wg sync.WaitGroup

		for remoteID, peer := range r.Peers() {
			wg.Add(2)

			r.switchesLock.Lock()
			addrs := []string{}
			swIDs := []string{}
			for swID, sw := range r.switches {
				// Don't test latency to self
				if swID == remoteID {
					continue
				}

				addrs = append(addrs, sw.Addr)
				swIDs = append(swIDs, swID)
			}
			r.switchesLock.Unlock()

			go func(remoteID string, peer SwitchRemote) {
				defer wg.Done()

				if r.verbose {
					log.Println("Starting latency tests for switch with ID", remoteID)
				}

				benchmarkClientCertPEM, benchmarkClientPrivKeyPEM, err := utils.GenerateCertificate(r.caCfg, r.caPrivKey, r.benchmarkClientCertValidity, "", "", utils.RoleBenchmarkClient)
				if err != nil {
					return
				}

				testResults, err := peer.TestLatency(nil, r.testTimeout, addrs, CertPair{
					CertPEM:        benchmarkClientCertPEM,
					CertPrivKeyPEM: benchmarkClientPrivKeyPEM,
				})
				if err != nil {
					log.Println("Could not run latency test for switch with ID", remoteID, ", continuing:", err)

					return
				}

				if len(testResults) < len(addrs) {
					log.Printf("%v: for ID %v, continuing", ErrInvalidLatencyTestResultLength, remoteID)

					return
				}

				results := map[string]time.Duration{}
				for i, swID := range swIDs {
					results[swID] = testResults[i]
				}

				r.switchesLock.Lock()

				sm, ok := r.switches[remoteID]
				if !ok {
					r.switchesLock.Unlock()

					return
				}

				sm.Latencies = results

				r.switches[remoteID] = sm

				r.switchesLock.Unlock()

				if r.verbose {
					log.Println("Finished latency tests for switch with ID", remoteID, ":", sm.Latencies)
				}

				if err := r.updateGraphs(context.Background()); err != nil {
					log.Println("Could not update graph, continuing:", err)
				}
			}(remoteID, peer)

			go func(remoteID string, peer SwitchRemote) {
				defer wg.Done()

				if r.verbose {
					log.Println("Starting throughput tests for switch with ID", remoteID)
				}

				benchmarkClientCertPEM, benchmarkClientPrivKeyPEM, err := utils.GenerateCertificate(r.caCfg, r.caPrivKey, r.benchmarkClientCertValidity, "", "", utils.RoleBenchmarkClient)
				if err != nil {
					return
				}

				testResults, err := peer.TestThroughput(nil, r.testTimeout, addrs, r.throughputLength, r.throughputChunks, CertPair{
					CertPEM:        benchmarkClientCertPEM,
					CertPrivKeyPEM: benchmarkClientPrivKeyPEM,
				})
				if err != nil {
					log.Println("Could not run throughput test for switch with ID", remoteID, ", continuing:", err)

					return
				}

				if len(testResults) < len(addrs) {
					log.Printf("%v: for ID %v, continuing", ErrInvalidThroughputTestResultLength, remoteID)

					return
				}

				results := map[string]ThroughputResult{}
				for i, swID := range swIDs {
					results[swID] = testResults[i]
				}

				r.switchesLock.Lock()

				sm, ok := r.switches[remoteID]
				if !ok {
					r.switchesLock.Unlock()

					return
				}

				sm.Throughputs = results

				r.switches[remoteID] = sm

				r.switchesLock.Unlock()

				if r.verbose {
					log.Println("Finished throughput tests for switch with ID", remoteID, ":", sm.Throughputs)
				}

				if err := r.updateGraphs(context.Background()); err != nil {
					log.Println("Could not update graph, continuing:", err)
				}
			}(remoteID, peer)
		}

		wg.Wait()
	}
}

func (r *Router) getSwitches() map[string]SwitchMetadata {
	r.switchesLock.Lock()
	defer r.switchesLock.Unlock()

	a := map[string]SwitchMetadata{}
	for k, v := range r.switches {
		a[k] = v
	}

	return a
}

func (r *Router) provisionRoute(srcID, dstID, routeID string) error {
	if r.verbose {
		log.Println("Provisioning route from", srcID, "to", dstID, "with route ID", routeID)
	}

	r.graphLock.Lock()

	path, err := graph.ShortestPath(r.graph, srcID, dstID)
	if err != nil {
		r.graphLock.Unlock()

		return err
	}

	if len(path) < 3 {
		r.graphLock.Unlock()

		return ErrRouteNotFound
	}

	r.graphLock.Unlock()

	routerPeers := r.Peers()
	switches := r.getSwitches()

	switchesToProvision := []SwitchRemote{}
	switchMetadata := []SwitchMetadata{}
	for _, swID := range path[1 : len(path)-1] {
		sw, ok := routerPeers[swID]
		if !ok {
			return ErrSwitchNotFound
		}

		md, ok := switches[swID]
		if !ok {
			return ErrSwitchNotFound
		}

		switchesToProvision = append([]SwitchRemote{sw}, switchesToProvision...)
		switchMetadata = append([]SwitchMetadata{md}, switchMetadata...)
	}

	egressLaddr := ""
	ingressRaddr := ""
	for i, sw := range switchesToProvision {
		publicIP, err := sw.GetPublicIP(context.Background())
		if err != nil {
			return err
		}

		var (
			switchListenCertPEM,
			switchListenCertPrivKeyPEM,
			switchClientCertPEM,
			switchClientCertPrivKeyPEM,
			adapterListenCertPEM,
			adapterListenCertPrivKeyPEM []byte
		)

		// Create a switch listen certificate for all but the last switch in the chain
		if i != len(switchesToProvision)-1 {
			switchListenCertPEM, switchListenCertPrivKeyPEM, err = utils.GenerateCertificate(r.caCfg, r.caPrivKey, r.callCertValidity, routeID, publicIP, utils.RoleSwitchListener)
			if err != nil {
				return err
			}
		}

		// Create a switch client certificate for all but the first switch in the chain
		if i == 0 && i != len(switchesToProvision)-1 {
			switchClientCertPEM, switchClientCertPrivKeyPEM, err = utils.GenerateCertificate(r.caCfg, r.caPrivKey, r.callCertValidity, routeID, "", utils.RoleSwitchClient)
			if err != nil {
				return err
			}
		}

		// Create an adapter listen certificate for the first and last switches in the chain
		if i == 0 || i == len(switchesToProvision)-1 {
			adapterListenCertPEM, adapterListenCertPrivKeyPEM, err = utils.GenerateCertificate(r.caCfg, r.caPrivKey, r.callCertValidity, routeID, publicIP, utils.RoleAdapterListener)
			if err != nil {
				return err
			}
		}

		laddrs, err := sw.ProvisionRoute(
			context.Background(),
			routeID,
			ingressRaddr,
			CertPair{
				CertPEM:        switchListenCertPEM,
				CertPrivKeyPEM: switchListenCertPrivKeyPEM,
			},
			CertPair{
				CertPEM:        switchClientCertPEM,
				CertPrivKeyPEM: switchClientCertPrivKeyPEM,
			},
			CertPair{
				CertPEM:        adapterListenCertPEM,
				CertPrivKeyPEM: adapterListenCertPrivKeyPEM,
			},
		)
		if err != nil {
			return err
		}

		newRaddr, err := net.ResolveTCPAddr("tcp", switchMetadata[i].Addr)
		if err != nil {
			return err
		}

		if i == 0 {
			if len(laddrs) != 2 {
				return ErrInvalidPortsCount
			}

			newLaddr, err := net.ResolveTCPAddr("tcp", laddrs[0])
			if err != nil {
				return err
			}

			newRaddr.Port = newLaddr.Port

			egressLaddr = newRaddr.String()

			laddrs = []string{laddrs[1]}
		} else {
			if len(laddrs) != 1 {
				return ErrInvalidPortsCount
			}
		}

		newLaddr, err := net.ResolveTCPAddr("tcp", laddrs[0])
		if err != nil {
			return err
		}

		newRaddr.Port = newLaddr.Port

		ingressRaddr = newRaddr.String()
	}

	adapters := r.Gateway.Peers()

	dst, ok := adapters[path[0]]
	if !ok {
		return ErrAdapterNotFound
	}

	src, ok := adapters[path[len(path)-1]]
	if !ok {
		return ErrAdapterNotFound
	}

	adapterDstCertPEM, adapterDstCertPrivKeyPEM, err := utils.GenerateCertificate(r.caCfg, r.caPrivKey, r.callCertValidity, routeID, "", utils.RoleAdapterClient)
	if err != nil {
		return err
	}

	if err := dst.ProvisionRoute(
		context.Background(),
		routeID,
		egressLaddr,
		CertPair{
			CertPEM:        adapterDstCertPEM,
			CertPrivKeyPEM: adapterDstCertPrivKeyPEM,
		},
	); err != nil {
		return err
	}

	adapterSrcCertPEM, adapterSrcCertPrivKeyPEM, err := utils.GenerateCertificate(r.caCfg, r.caPrivKey, r.callCertValidity, routeID, "", utils.RoleAdapterClient)
	if err != nil {
		return err
	}

	if err := src.ProvisionRoute(
		context.Background(),
		routeID,
		ingressRaddr,
		CertPair{
			CertPEM:        adapterSrcCertPEM,
			CertPrivKeyPEM: adapterSrcCertPrivKeyPEM,
		},
	); err != nil {
		return err
	}

	r.routesLock.Lock()
	r.routes[routeID] = path
	r.routesLock.Unlock()

	if err := r.updateGraphs(context.Background()); err != nil {
		return err
	}

	return nil
}

func unprovisionRouteForPeer(r *Router, g *Gateway, remoteID string) error {
	if r.verbose {
		log.Println("Unprovisioning all routes for peer", remoteID)
	}

	r.routesLock.Lock()

	routerPeers := r.Peers()
	gatewayPeers := g.Peers()

	switchesToClose := map[string][]SwitchRemote{}
	adaptersToClose := map[string][]AdapterRemote{}

	for routeID, route := range r.routes {
		if slices.Contains(route, remoteID) {
			for _, candidateID := range route {
				if remoteID == candidateID {
					// Don't call `close` on the peer with `remoteID` as its already disconnected at this point
					continue
				}

				if sw, ok := routerPeers[candidateID]; ok {
					if _, ok := switchesToClose[routeID]; !ok {
						switchesToClose[routeID] = []SwitchRemote{}
					}

					switchesToClose[routeID] = append(switchesToClose[routeID], sw)
				}

				if ad, ok := gatewayPeers[candidateID]; ok {
					if _, ok := adaptersToClose[routeID]; !ok {
						adaptersToClose[routeID] = []AdapterRemote{}
					}

					adaptersToClose[routeID] = append(adaptersToClose[routeID], ad)
				}
			}

			delete(r.routes, routeID)
		}
	}

	r.routesLock.Unlock()

	unprovisionSwitchesAndAdapters(switchesToClose, adaptersToClose, remoteID)

	if err := r.updateGraphs(context.Background()); err != nil {
		return err
	}

	return nil
}

func unprovisionSwitchesAndAdapters(switchesToClose map[string][]SwitchRemote, adaptersToClose map[string][]AdapterRemote, remoteID string) {
	var wg sync.WaitGroup

	for routeID, peers := range switchesToClose {
		for _, sw := range peers {
			wg.Add(1)

			go func(routeID string, sw SwitchRemote) {
				defer wg.Done()

				if err := sw.UnprovisionRoute(context.Background(), routeID); err != nil {
					log.Println("Could not unprovision route", routeID, "for switch with ID", remoteID, ", continuing:", err)
				}
			}(routeID, sw)
		}
	}

	for routeID, peers := range adaptersToClose {
		for _, ad := range peers {
			wg.Add(1)

			go func(routeID string, ad AdapterRemote) {
				defer wg.Done()

				if err := ad.UnprovisionRoute(context.Background(), routeID); err != nil {
					log.Println("Could not unprovision route", routeID, "for adapter with ID", remoteID, ", continuing:", err)
				}
			}(routeID, ad)
		}
	}

	wg.Wait()
}

func (r *Router) RegisterSwitch(ctx context.Context, token string, addr string) (SwitchConfiguration, error) {
	if err := r.auth.Validate(token); err != nil {
		return SwitchConfiguration{}, err
	}

	remoteID := rpc.GetRemoteID(ctx)

	parsedAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return SwitchConfiguration{}, err
	}

	r.switchesLock.Lock()

	if _, ok := r.switches[remoteID]; ok {
		r.switchesLock.Unlock()

		return SwitchConfiguration{}, ErrSwitchAlreadyRegistered
	}

	r.switches[remoteID] = SwitchMetadata{
		addr,
		map[string]time.Duration{},
		map[string]ThroughputResult{},
	}

	if r.verbose {
		log.Println("Added switch with ID", remoteID, "to topology")
	}

	r.switchesLock.Unlock()

	if err := r.updateGraphs(context.Background()); err != nil {
		return SwitchConfiguration{}, err
	}

	benchmarkListenCertPEM, benchmarkListenCertPrivKeyPEM, err := utils.GenerateCertificate(r.caCfg, r.caPrivKey, r.callCertValidity, "", parsedAddr.IP.String(), utils.RoleAdapterListener)
	if err != nil {
		return SwitchConfiguration{}, err
	}

	return SwitchConfiguration{
		CAPEM: r.caPEM,
		BenchmarkListenCert: CertPair{
			CertPEM:        benchmarkListenCertPEM,
			CertPrivKeyPEM: benchmarkListenCertPrivKeyPEM,
		},
	}, nil
}
