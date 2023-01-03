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
	RenderNetworkVisualization func(
		ctx context.Context,
		switches map[string]SwitchMetadata,
		adapters map[string]AdapterMetadata,
	) error
	RenderRoutesVisualization func(
		ctx context.Context,
		routes map[string][]string,
	) error
}

func createNetworkGraph(
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

	return g, nil
}

func createRoutesGraph(
	routes map[string][]string,
) (graph.Graph[string, string], error) {
	g := graph.New(graph.StringHash, graph.Directed())

	for routeID, route := range routes {
		for i, swID := range route {
			if _, err := g.Vertex(swID); err != nil {
				if errors.Is(err, graph.ErrEdgeNotFound) {
					label := fmt.Sprintf("Switch %v", swID)
					if i == 0 || i == len(route)-1 {
						label = fmt.Sprintf("Adapter %v", swID)
					}

					if err := g.AddVertex(swID, graph.VertexAttribute("label", label)); err != nil {
						return nil, err
					}
				} else {
					return nil, err
				}
			}

			if i != 0 {
				if err := g.AddEdge(swID, route[i-1], graph.EdgeAttribute("label", routeID)); err != nil {
					return nil, err
				}
			}
		}
	}

	return g, nil
}

type Visualizer struct {
	verbose bool
	format  string

	networkFile     *os.File
	networkFileLock sync.Mutex

	routesFile     *os.File
	routesFileLock sync.Mutex

	Peers func() map[string]MetricsRemote
}

func NewVisualizer(
	verbose bool,
	format string,
	networkFile *os.File,
	routesFile *os.File,
) *Visualizer {
	return &Visualizer{
		verbose:     verbose,
		format:      format,
		networkFile: networkFile,
		routesFile:  routesFile,
	}
}

func (v *Visualizer) RenderNetworkVisualization(
	ctx context.Context,
	switches map[string]SwitchMetadata,
	adapters map[string]AdapterMetadata,
) error {
	v.networkFileLock.Lock()
	defer v.networkFileLock.Unlock()

	remoteID := rpc.GetRemoteID(ctx)

	if v.verbose {
		log.Println("Rendering network graph visualization for metrics service with ID", remoteID)
	}

	g, err := createNetworkGraph(switches, adapters)
	if err != nil {
		return err
	}

	if err := v.networkFile.Truncate(0); err != nil {
		v.networkFileLock.Unlock()

		return err
	}

	if _, err := v.networkFile.Seek(0, 0); err != nil {
		v.networkFileLock.Unlock()

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

	return graphviz.New().Render(gv, graphviz.Format(v.format), v.networkFile)
}

func (v *Visualizer) RenderRoutesVisualization(
	ctx context.Context,
	routes map[string][]string,
) error {
	v.routesFileLock.Lock()
	defer v.routesFileLock.Unlock()

	remoteID := rpc.GetRemoteID(ctx)

	if v.verbose {
		log.Println("Rendering routes graph visualization for metrics service with ID", remoteID)
	}

	g, err := createRoutesGraph(routes)
	if err != nil {
		return err
	}

	if err := v.routesFile.Truncate(0); err != nil {
		v.routesFileLock.Unlock()

		return err
	}

	if _, err := v.routesFile.Seek(0, 0); err != nil {
		v.routesFileLock.Unlock()

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

	return graphviz.New().Render(gv, graphviz.Format(v.format), v.routesFile)
}
