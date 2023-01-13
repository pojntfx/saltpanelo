package auth

import (
	"context"
	"errors"
	"net/url"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

var (
	ErrEmptyOIDCClientSecret = errors.New("could not continue with empty OIDC client secret")
)

// Extended from https://github.com/pojntfx/goit/blob/main/pkg/token/token.go
type TokenManagerClientCredentials struct {
	oidcIssuer       string
	oidcClientID     string
	oidcClientSecret string
	oidcAudience     string

	tokenSource oauth2.TokenSource

	ctx context.Context
}

func NewTokenManagerClientCredentials(
	oidcIssuer string,
	oidcClientID string,
	oidcClientSecret string,
	oidcAudience string,

	ctx context.Context,
) *TokenManagerClientCredentials {
	return &TokenManagerClientCredentials{
		oidcIssuer:       oidcIssuer,
		oidcClientID:     oidcClientID,
		oidcClientSecret: oidcClientSecret,
		oidcAudience:     oidcAudience,

		ctx: ctx,
	}
}

func (t *TokenManagerClientCredentials) InitialLogin() error {
	provider, err := oidc.NewProvider(t.ctx, t.oidcIssuer)
	if err != nil {
		return err
	}

	endpointParams := url.Values{}
	endpointParams.Set("audience", t.oidcAudience)

	config := &clientcredentials.Config{
		ClientID:       t.oidcClientID,
		ClientSecret:   t.oidcClientSecret,
		TokenURL:       provider.Endpoint().TokenURL,
		Scopes:         []string{},
		EndpointParams: endpointParams,
	}

	t.tokenSource = config.TokenSource(t.ctx)

	return nil
}

func (t *TokenManagerClientCredentials) GetIDToken() (string, error) {
	if t.tokenSource == nil {
		return "", ErrNotLoggedIn
	}

	token, err := t.tokenSource.Token()
	if err != nil {
		return "", err
	}

	return token.AccessToken, nil
}
