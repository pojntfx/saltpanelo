package backends

import "context"

type Adapter struct{}

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
	return &Adapter{}
}

func (a *Adapter) Login() error {
	return nil
}

func (a *Adapter) Link() error {
	return nil
}

func (a *Adapter) RequestCall(email, channelID string) (bool, error) {
	return true, nil
}
