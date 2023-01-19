package backends

import (
	"context"
	"log"
)

type Adapter struct {
	onRequestCall      func(ctx context.Context, srcID, srcEmail, routeID, channelID string) (bool, error)
	onCallDisconnected func(ctx context.Context, routeID string) error
	onHandleCall       func(ctx context.Context, routeID, raddr string) error
	openURL            func(url string) error

	raddr,
	ahost string
	verbose bool
	timeout int

	oidcIssuer,
	oidcClientID,
	oidcRedirectURL string
}

func NewAdapter(
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
		onRequestCall,
		onCallDisconnected,
		onHandleCall,
		openURL,

		raddr,
		ahost,
		verbose,
		timeout,

		oidcIssuer,
		oidcClientID,
		oidcRedirectURL,
	}
}

func (a *Adapter) Login() error {
	log.Println("Testing onRequestCall ...")

	accept, err := a.onRequestCall(context.Background(), "srcIDTest", "srcEmailTest", "routeIDTest", "channelIDTest")

	log.Println("Received for onRequestCall:", accept, err)

	log.Println("Testing onCallDisconnected ...")

	err = a.onCallDisconnected(context.Background(), "routeIDTest")

	log.Println("Received for onCallDisconnected:", err)

	log.Println("Testing onHandleCall ...")

	err = a.onHandleCall(context.Background(), "routeIDTest", "raddrTest")

	log.Println("Received for onHandleCall:", err)

	log.Println("Testing openURL ...")

	err = a.openURL("urlTest")

	log.Println("Received for openURL:", err)

	return nil
}

func (a *Adapter) Link() error {
	return nil
}

func (a *Adapter) RequestCall(email, channelID string) (bool, error) {
	return true, nil
}
