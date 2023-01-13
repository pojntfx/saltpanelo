package services

import (
	"context"
	"log"

	"github.com/pojntfx/saltpanelo/pkg/auth"
)

type MetricsRemote struct{}

func HandleMetricsClientConnect(router *Router) error {
	return router.updateGraphs(context.Background())
}

type Metrics struct {
	verbose bool

	auth            *auth.OIDCAuthn
	authorizedEmail string

	Peers func() map[string]VisualizerRemote
}

func NewMetrics(
	verbose bool,
	oidcIssuer,
	oidcClientID,
	authorizedEmail string,
) *Metrics {
	return &Metrics{
		verbose: verbose,

		auth:            auth.NewOIDCAuthn(oidcIssuer, oidcClientID),
		authorizedEmail: authorizedEmail,
	}
}

func (m *Metrics) Open(ctx context.Context) error {
	return m.auth.Open(ctx)
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

		token, err := peer.Attest(ctx)
		if err != nil {
			log.Println("Could not attest peer with ID", remoteID, ", skipping")

			continue
		}

		email, err := m.auth.Validate(token)
		if err != nil {
			log.Println("Could not attest peer with ID", remoteID, ", skipping")

			continue
		}

		if email != m.authorizedEmail {
			log.Println("Could not attest peer with ID", remoteID, ", skipping")

			continue
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
