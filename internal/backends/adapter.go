package backends

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/pojntfx/dudirekta/pkg/rpc"
	"github.com/pojntfx/saltpanelo/pkg/auth"
	"github.com/pojntfx/saltpanelo/pkg/services"
	"nhooyr.io/websocket"
)

var (
	ErrNotReady = errors.New("adapter not ready")
)

type Adapter struct {
	ctx context.Context

	onRequestCall      func(ctx context.Context, srcID, srcEmail, routeID, channelID string) (bool, error)
	onCallDisconnected func(ctx context.Context, routeID string) error
	onHandleCall       func(ctx context.Context, routeID, raddr string) error

	raddr,
	ahost string
	verbose bool
	timeout int

	tm *auth.TokenManagerAuthorizationCode

	peers func() map[string]services.GatewayRemote
}

func NewAdapter(
	ctx context.Context,

	onRequestCall func(ctx context.Context, srcID, srcEmail, routeID, channelID string) (bool, error),
	onCallDisconnected func(ctx context.Context, routeID string) error,
	onHandleCall func(ctx context.Context, routeID, raddr string) error,
	openURL func(url string) error,

	raddr,
	ahost string,
	verbose bool,
	timeout int,

	oidcIssuer,
	oidcClientID,
	oidcRedirectURL string,
) *Adapter {
	return &Adapter{
		ctx,

		onRequestCall,
		onCallDisconnected,
		onHandleCall,

		raddr,
		ahost,
		verbose,
		timeout,

		auth.NewTokenManagerAuthorizationCode(
			oidcIssuer,
			oidcClientID,
			oidcRedirectURL,

			func(s string) error {
				if err := openURL(s); err != nil {
					log.Printf(`Could not open browser, please open the following URL in your browser manually to authorize:
%v`, s)
				}

				return nil
			},

			ctx,
		),

		nil,
	}
}

func (a *Adapter) Login() error {
	return a.tm.InitialLogin()
}

func (a *Adapter) Link() error {
	errs := make(chan error)

	l := services.NewAdapter(
		a.verbose,
		a.ahost,
		a.onRequestCall,
		a.onCallDisconnected,
		a.onHandleCall,
		a.tm.GetIDToken,
	)
	clients := 0
	registry := rpc.NewRegistry(
		l,
		services.GatewayRemote{},
		time.Millisecond*time.Duration(a.timeout),
		a.ctx,
		&rpc.Options{
			ResponseBufferLen: rpc.DefaultResponseBufferLen,
			OnClientConnect: func(remoteID string) {
				clients++

				if a.verbose {
					log.Printf("%v clients connected", clients)
				}

				go func() {
					for candidateID, peer := range l.Peers() {
						if remoteID == candidateID {
							if a.verbose {
								log.Println("Registering with gateway with ID", remoteID)
							}

							token, err := a.tm.GetIDToken()
							if err != nil {
								errs <- err

								return
							}

							caPEM, err := peer.RegisterAdapter(a.ctx, token)
							if err != nil {
								errs <- err

								return
							}

							services.SetAdapterCA(l, caPEM)

							if a.verbose {
								log.Println("Registered with gateway with ID", remoteID)
							}
						}
					}
				}()
			},
			OnClientDisconnect: func(remoteID string) {
				clients--

				if a.verbose {
					log.Printf("%v clients connected", clients)
				}
			},
		},
	)
	l.Peers = registry.Peers
	a.peers = l.Peers

	go func() {
		rawConn, _, err := websocket.Dial(a.ctx, a.raddr, nil)
		if err != nil {
			errs <- err

			return
		}
		conn := websocket.NetConn(a.ctx, rawConn, websocket.MessageText)
		defer conn.Close()

		if a.verbose {
			log.Println("Connected to", conn.RemoteAddr())
		}

		if err := registry.Link(conn); err != nil {
			errs <- err

			return
		}
	}()

	return <-errs
}

func (a *Adapter) RequestCall(email, channelID string) (bool, error) {
	if a.peers == nil {
		return false, ErrNotReady
	}

	for _, peer := range a.peers() {
		token, err := a.tm.GetIDToken()
		if err != nil {
			return false, err
		}

		dstID, err := peer.ResolveEmailToID(a.ctx, token, email)
		if err != nil {
			return false, err
		}

		requestCallResult, err := peer.RequestCall(a.ctx, token, dstID, channelID)
		if err != nil {
			return false, err
		}

		return requestCallResult.Accept, nil
	}

	return true, nil
}
