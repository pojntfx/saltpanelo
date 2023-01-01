package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"sync"

	"github.com/dominikbraun/graph"
	"github.com/dominikbraun/graph/draw"
	"github.com/goccy/go-graphviz"
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

	switchKeys := []string{}
	for k := range switches {
		switchKeys = append(switchKeys, k)
	}
	sort.Strings(switchKeys)

	for _, swID := range switchKeys {
		if err := g.AddVertex(swID, graph.VertexAttribute("label", fmt.Sprintf("Switch %v", swID))); err != nil {
			return nil, err
		}
	}

	for _, swID := range switchKeys {
		for _, candidateID := range switchKeys {
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

	adapterKeys := []string{}
	for k := range adapters {
		adapterKeys = append(adapterKeys, k)
	}
	sort.Strings(adapterKeys)

	for _, aID := range adapterKeys {
		if err := g.AddVertex(aID, graph.VertexAttribute("label", fmt.Sprintf("Adapter %v", aID))); err != nil {
			return nil, err
		}

		for swID, latency := range adapters[aID].Latencies {
			weight := int(latency.Nanoseconds() + adapters[aID].Throughputs[swID].Read.Milliseconds() + adapters[aID].Throughputs[swID].Write.Milliseconds())

			if err := g.AddEdge(swID, aID, graph.EdgeWeight(weight), graph.EdgeAttribute("label", fmt.Sprint(weight))); err != nil {
				if errors.Is(err, graph.ErrVertexNotFound) {
					continue
				}

				return nil, err
			}

			if err := g.AddEdge(aID, swID, graph.EdgeWeight(weight), graph.EdgeAttribute("label", fmt.Sprint(weight))); err != nil {
				if errors.Is(err, graph.ErrVertexNotFound) {
					continue
				}

				return nil, err
			}
		}
	}

	// TODO: Add links between adapters and switches

	return g, nil
}

type Visualizer struct {
	verbose bool
	format  string

	writer     *os.File
	writerLock sync.Mutex

	Peers func() map[string]MetricsRemote
}

func NewVisualizer(verbose bool, format string, writer *os.File) *Visualizer {
	return &Visualizer{
		verbose: verbose,
		format:  format,
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

	buf := &bytes.Buffer{}
	if err := draw.DOT(g, buf); err != nil {
		return err
	}

	gv, err := graphviz.ParseBytes(buf.Bytes())
	if err != nil {
		return err
	}

	return graphviz.New().Render(gv, graphviz.Format(v.format), v.writer)
}
