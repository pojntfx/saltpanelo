package services

import (
	"context"
	"log"
	"os"
	"sync"

	"github.com/pojntfx/dudirekta/pkg/rpc"
)

type visualizerWriter struct {
	VisualizerRemote

	ctx context.Context
}

func (v visualizerWriter) Write(p []byte) (n int, err error) {
	return v.WriteVisualization(v.ctx, p)
}

type VisualizerRemote struct {
	WriteVisualization  func(ctx context.Context, p []byte) (n int, err error)
	NewVisualization    func(ctx context.Context) error
	FinishVisualization func(ctx context.Context) error
}

type Visualizer struct {
	verbose bool
	writer  *os.File

	writerLock sync.Mutex

	Peers func() map[string]MetricsRemote
}

func NewVisualizer(verbose bool, writer *os.File) *Visualizer {
	return &Visualizer{
		verbose: verbose,
		writer:  writer,
	}
}

func (v *Visualizer) NewVisualization(ctx context.Context) (err error) {
	v.writerLock.Lock()

	remoteID := rpc.GetRemoteID(ctx)

	if v.verbose {
		log.Println("Creating new graph visualization for metrics service with ID", remoteID)
	}

	if err := v.writer.Truncate(0); err != nil {
		v.writerLock.Unlock()

		return err
	}

	if _, err := v.writer.Seek(0, 0); err != nil {
		v.writerLock.Unlock()

		return err
	}

	return nil
}

func (v *Visualizer) WriteVisualization(ctx context.Context, p []byte) (n int, err error) {
	remoteID := rpc.GetRemoteID(ctx)

	if v.verbose {
		log.Println("Writing graph visualization for metrics service with ID", remoteID)
	}

	n, err = v.writer.Write(p)
	if err != nil {
		v.writerLock.Unlock()

		return -1, err
	}

	return n, nil
}

func (v *Visualizer) FinishVisualization(ctx context.Context) (err error) {
	remoteID := rpc.GetRemoteID(ctx)

	if v.verbose {
		log.Println("Finishing new graph visualization for metrics service with ID", remoteID)
	}

	// TODO: Prevent calling `unlock` if already unlocked
	v.writerLock.Unlock()

	return nil
}
