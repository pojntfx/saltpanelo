package services

import (
	"context"
	"log"
)

type MetricsRemote struct{}

func HandleMetricsClientConnect(router *Router) error {
	return router.updateGraphs(context.Background())
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
	routes map[string][]string,
) error {
	for remoteID, peer := range m.Peers() {
		if m.verbose {
			log.Println("Visualizing graph for peer with ID", remoteID)
		}

		if err := peer.RenderNetworkVisualization(ctx, switches, adapters); err != nil {
			return err
		}

		if err := peer.RenderRoutesVisualization(ctx, routes); err != nil {
			return err
		}
	}

	return nil
}
