package services

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pojntfx/dudirekta/pkg/rpc"
	"github.com/pojntfx/saltpanelo/pkg/auth"
	"github.com/pojntfx/saltpanelo/pkg/utils"
)

var (
	ErrInvalidLatencyTestResultLength    = errors.New("received invalid length of latency test results")
	ErrInvalidThroughputTestResultLength = errors.New("received invalid length of throughput test results")
	ErrSrcNotFound                       = errors.New("could not find source")
	ErrAdapterNotFound                   = errors.New("could not find adapter")
)

type GatewayRemote struct {
	RegisterAdapter  func(ctx context.Context, token string) ([]byte, error)
	RequestCall      func(ctx context.Context, token string, dstID, channelID string) (RequestCallResult, error)
	HangupCall       func(ctx context.Context, token string, routeID string) error
	ResolveEmailToID func(ctx context.Context, token string, email string) (string, error)
}

type RequestCallResult struct {
	Accept  bool
	RouteID string
}

func HandleGatewayClientDisconnect(r *Router, g *Gateway, remoteID string) error {
	go func() {
		if err := unprovisionRouteForPeer(r, g, remoteID); err != nil {
			log.Println("Could not unprovision route for switch with ID", remoteID, ", continuing:", err)
		}
	}()

	return g.onClientDisconnect(remoteID)
}

type AdapterMetadata struct {
	Latencies   map[string]time.Duration
	Throughputs map[string]ThroughputResult
	UserEmail   string
}

type Gateway struct {
	verbose bool

	adaptersLock sync.Mutex
	adapters     map[string]AdapterMetadata

	auth *auth.OIDCAuthn

	caCfg     *x509.Certificate
	caPEM     []byte
	caPrivKey *rsa.PrivateKey

	benchmarkClientCertValidity time.Duration

	rsaBits int

	benchmarkLimit int64

	Router *Router

	Peers func() map[string]AdapterRemote
}

func NewGateway(
	verbose bool,

	oidcIssuer,
	oidcClientID string,

	caCfg *x509.Certificate,
	caPEM []byte,
	caPrivKey *rsa.PrivateKey,

	benchmarkClientCertValidity time.Duration,

	rsaBits int,

	benchmarkLimit int64,
) *Gateway {
	return &Gateway{
		verbose: verbose,

		adapters: map[string]AdapterMetadata{},

		auth: auth.NewOIDCAuthn(oidcIssuer, oidcClientID),

		caCfg:     caCfg,
		caPEM:     caPEM,
		caPrivKey: caPrivKey,

		benchmarkClientCertValidity: benchmarkClientCertValidity,

		rsaBits: rsaBits,
	}
}

func (g *Gateway) Open(ctx context.Context) error {
	return g.auth.Open(ctx)
}

func (g *Gateway) onClientDisconnect(remoteID string) error {
	g.adaptersLock.Lock()

	delete(g.adapters, remoteID)

	g.adaptersLock.Unlock()

	if g.verbose {
		log.Println("Removed adapter with ID", remoteID, "from topology")
	}

	return g.Router.updateGraphs(context.Background())
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
	benchmarkClientCertPEM, benchmarkClientPrivKeyPEM, err := utils.GenerateCertificate(g.rsaBits, g.caCfg, g.caPrivKey, g.benchmarkClientCertValidity, "", "", utils.RoleBenchmarkClient)
	if err != nil {
		return err
	}

	rawLatencies, err := remote.TestLatency(ctx, g.Router.testTimeout, addrs, CertPair{
		CertPEM:        benchmarkClientCertPEM,
		CertPrivKeyPEM: benchmarkClientPrivKeyPEM,
	})
	if err != nil {
		return err
	}

	rawThroughputs, err := remote.TestThroughput(
		ctx,
		g.Router.testTimeout,
		addrs,
		CertPair{
			CertPEM:        benchmarkClientCertPEM,
			CertPrivKeyPEM: benchmarkClientPrivKeyPEM,
		},
		g.benchmarkLimit,
	)
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

func (g *Gateway) RegisterAdapter(ctx context.Context, token string) ([]byte, error) {
	email, err := g.auth.Validate(token)
	if err != nil {
		return []byte{}, err
	}

	remoteID := rpc.GetRemoteID(ctx)

	g.adaptersLock.Lock()

	if _, ok := g.adapters[remoteID]; ok {
		g.adaptersLock.Unlock()

		return []byte{}, ErrAdapterAlreadyRegistered
	}

	g.adapters[remoteID] = AdapterMetadata{
		map[string]time.Duration{},
		map[string]ThroughputResult{},
		email,
	}

	if g.verbose {
		log.Println("Added adapter with ID", remoteID, "to topology")
	}

	g.adaptersLock.Unlock()

	if err := g.Router.updateGraphs(context.Background()); err != nil {
		return []byte{}, err
	}

	return g.caPEM, nil
}

func (g *Gateway) RequestCall(ctx context.Context, token string, dstID, channelID string) (RequestCallResult, error) {
	if _, err := g.auth.Validate(token); err != nil {
		return RequestCallResult{}, err
	}

	remoteID := rpc.GetRemoteID(ctx)
	routeID := uuid.NewString()

	g.adaptersLock.Lock()

	if g.verbose {
		log.Println("Remote with ID", remoteID, "is requesting a call with ID", dstID, "with route ID", routeID, "and channel ID", channelID)
	}

	sm, ok := g.adapters[remoteID]
	if !ok {
		g.adaptersLock.Unlock()

		return RequestCallResult{}, ErrAdapterNotFound
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
		sm.UserEmail,
		routeID,
		channelID,
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

	if err := g.Router.updateGraphs(context.Background()); err != nil {
		return RequestCallResult{}, err
	}

	if err := g.Router.provisionRoute(remoteID, dstID, routeID); err != nil {
		return RequestCallResult{}, err
	}

	return RequestCallResult{
		Accept:  true,
		RouteID: routeID,
	}, nil
}

func (g *Gateway) HangupCall(ctx context.Context, token string, routeID string) error {
	if _, err := g.auth.Validate(token); err != nil {
		return err
	}

	remoteID := rpc.GetRemoteID(ctx)

	if g.verbose {
		log.Println("Unprovisioning route with route ID", routeID)
	}

	g.Router.routesLock.Lock()

	routerPeers := g.Router.Peers()
	gatewayPeers := g.Peers()

	switchesToClose := map[string][]SwitchRemote{}
	adaptersToClose := map[string][]AdapterRemote{}

	route, ok := g.Router.routes[routeID]
	if !ok {
		g.Router.routesLock.Unlock()

		return ErrRouteNotFound
	}

	for _, candidateID := range route {
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

	delete(g.Router.routes, routeID)

	g.Router.routesLock.Unlock()

	unprovisionSwitchesAndAdapters(switchesToClose, adaptersToClose, remoteID)

	if err := g.Router.updateGraphs(context.Background()); err != nil {
		return err
	}

	return nil
}

func (g *Gateway) ResolveEmailToID(ctx context.Context, token string, email string) (string, error) {
	if _, err := g.auth.Validate(token); err != nil {
		return "", err
	}

	if g.verbose {
		log.Println("Looking up ID for email", email)
	}

	g.adaptersLock.Lock()
	defer g.adaptersLock.Unlock()

	id := ""
	for candidateID, sm := range g.adapters {
		if sm.UserEmail == email {
			id = candidateID

			break
		}
	}

	if strings.TrimSpace(id) == "" {
		return "", ErrAdapterNotFound
	}

	return id, nil
}
