package services

import (
	"context"
	"log"
	"os"
	"sync"

	"github.com/dominikbraun/graph"
	"github.com/dominikbraun/graph/draw"
	"github.com/pojntfx/dudirekta/pkg/rpc"
)

func createGraph(
	switches map[string]SwitchMetadata,
	adapters map[string]struct{},
) (graph.Graph[string, string], error) {
	g := graph.New(graph.StringHash, graph.Directed(), graph.Weighted())

	for swID := range switches {
		if err := g.AddVertex(swID); err != nil {
			return nil, err
		}
	}

	for swID := range switches {
		for candidateID := range switches {
			// Don't link to self
			if swID == candidateID {
				continue
			}

			// TODO: Also add throughput as weight
			latency, ok := switches[swID].Latencies[candidateID]
			if !ok {
				continue
			}

			if err := g.AddEdge(swID, candidateID, graph.EdgeWeight(int(latency.Nanoseconds()))); err != nil {
				return nil, err
			}
		}
	}

	for aID := range adapters {
		if err := g.AddVertex(aID); err != nil {
			return nil, err
		}
	}

	return g, nil
}

type VisualizerRemote struct {
	RenderVisualization func(
		ctx context.Context,
		switches map[string]SwitchMetadata,
		adapters map[string]struct{},
	) error
}

type Visualizer struct {
	verbose bool

	writer     *os.File
	writerLock sync.Mutex

	Peers func() map[string]MetricsRemote
}

func NewVisualizer(verbose bool, writer *os.File) *Visualizer {
	return &Visualizer{
		verbose: verbose,
		writer:  writer,
	}
}

func (v *Visualizer) RenderVisualization(
	ctx context.Context,
	switches map[string]SwitchMetadata,
	adapters map[string]struct{},
) error {
	v.writerLock.Lock()
	defer v.writerLock.Unlock()

	remoteID := rpc.GetRemoteID(ctx)

	if v.verbose {
		log.Println("Rendering graph visualization for metrics service with ID", remoteID)
	}

	g, err := createGraph(switches, adapters)
	if err != nil {
		return err
	}

	if err := v.writer.Truncate(0); err != nil {
		v.writerLock.Unlock()

		return err
	}

	if _, err := v.writer.Seek(0, 0); err != nil {
		v.writerLock.Unlock()

		return err
	}

	return draw.DOT(g, v.writer)
}
