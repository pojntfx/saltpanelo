package services

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/dominikbraun/graph"
	"github.com/dominikbraun/graph/draw"
	"github.com/pojntfx/dudirekta/pkg/rpc"
)

type VisualizerRemote struct {
	RenderVisualization func(
		ctx context.Context,
		switches map[string]SwitchMetadata,
		adapters map[string]AdapterMetadata,
	) error
}

func createGraph(
	switches map[string]SwitchMetadata,
	adapters map[string]AdapterMetadata,
) (graph.Graph[string, string], error) {
	g := graph.New(graph.StringHash, graph.Directed(), graph.Weighted())

	for swID := range switches {
		if err := g.AddVertex(swID, graph.VertexAttribute("label", fmt.Sprintf("Switch %v", swID))); err != nil {
			return nil, err
		}
	}

	for swID := range switches {
		for candidateID := range switches {
			// Don't link to self
			if swID == candidateID {
				continue
			}

			latency, ok := switches[swID].Latencies[candidateID]
			if !ok {
				continue
			}

			throughput, ok := switches[swID].Throughputs[candidateID]
			if !ok {
				continue
			}

			weight := int(latency.Nanoseconds() + throughput.Read.Milliseconds() + throughput.Write.Milliseconds())

			if err := g.AddEdge(swID, candidateID, graph.EdgeWeight(weight), graph.EdgeAttribute("label", fmt.Sprint(weight))); err != nil {
				return nil, err
			}
		}
	}

	for aID := range adapters {
		if err := g.AddVertex(aID, graph.VertexAttribute("label", fmt.Sprintf("Adapter %v", aID))); err != nil {
			return nil, err
		}
	}

	// TODO: Add links between adapters and switches

	return g, nil
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
	adapters map[string]AdapterMetadata,
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
