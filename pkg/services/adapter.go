package services

type AdapterRemote struct{}

type Adapter struct {
	verbose bool

	Peers func() map[string]GatewayRemote
}

func NewAdapter(verbose bool) *Adapter {
	return &Adapter{
		verbose: verbose,
	}
}
