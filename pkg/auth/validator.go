package auth

import (
	"context"
	"errors"

	"github.com/coreos/go-oidc/v3/oidc"
)

var (
	ErrClosed = errors.New("authenticator has not been opened")
)

type Authn struct {
	issuer   string
	clientID string

	ctx context.Context

	verifier *oidc.IDTokenVerifier
}

func NewAuthn(issuer string, clientID string) *Authn {
	return &Authn{
		issuer:   issuer,
		clientID: clientID,
	}
}

func (a *Authn) Open(ctx context.Context) error {
	provider, err := oidc.NewProvider(ctx, a.issuer)
	if err != nil {
		return err
	}

	a.ctx = ctx
	a.verifier = provider.Verifier(&oidc.Config{ClientID: a.clientID})

	return nil
}

func (a *Authn) Validate(token string) (string, error) {
	if a.verifier == nil {
		return "", ErrNotLoggedIn
	}

	t, err := a.verifier.Verify(a.ctx, token)
	if err != nil {
		return "", err
	}

	var claims struct {
		Email string `json:"email"`
	}

	if err := t.Claims(&claims); err != nil {
		return "", err
	}

	return claims.Email, nil
}
