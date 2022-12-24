package services

import (
	"context"
	"log"

	"github.com/dominikbraun/graph"
	"github.com/dominikbraun/graph/draw"
)

type MetricsRemote struct{}

type Metrics struct {
	verbose bool

	Peers func() map[string]VisualizerRemote
}

func NewMetrics(verbose bool) *Metrics {
	return &Metrics{
		verbose: verbose,
	}
}

func (m *Metrics) visualize(ctx context.Context, g graph.Graph[string, string]) error {
	for remoteID, peer := range m.Peers() {
		if m.verbose {
			log.Println("Visualizing graph for peer with ID", remoteID)
		}

		if err := peer.NewVisualization(ctx); err != nil {
			return err
		}

		w := visualizerWriter{peer, ctx}

		if err := draw.DOT(g, w); err != nil {
			if err := peer.FinishVisualization(ctx); err != nil {
				return err
			}

			return err
		}

		if err := peer.FinishVisualization(ctx); err != nil {
			return err
		}
	}

	return nil
}
