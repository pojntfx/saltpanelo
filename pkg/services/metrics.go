package services

import (
	"context"
	"log"
)

type MetricsRemote struct{}

func HandleMetricsClientConnect(router *Router) {
	router.updateGraph(context.Background())
}

type Metrics struct {
	verbose bool

	Peers func() map[string]VisualizerRemote
}

func NewMetrics(verbose bool) *Metrics {
	return &Metrics{
		verbose: verbose,
	}
}

func (m *Metrics) visualize(
	ctx context.Context,
	switches map[string]SwitchMetadata,
	adapters map[string]AdapterMetadata,
) error {
	for remoteID, peer := range m.Peers() {
		if m.verbose {
			log.Println("Visualizing graph for peer with ID", remoteID)
		}

		if err := peer.RenderVisualization(ctx, switches, adapters); err != nil {
			return err
		}
	}

	return nil
}
