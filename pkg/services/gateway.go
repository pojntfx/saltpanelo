package services

type GatewayRemote struct{}

type Gateway struct {
	verbose bool

	Switch *Switch
	Peers  func() map[string]RouterRemote
}

func NewGateway(verbose bool) *Gateway {
	return &Gateway{
		verbose: verbose,
	}
}
