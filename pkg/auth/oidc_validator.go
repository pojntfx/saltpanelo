package auth

import (
	"context"
	"errors"

	"github.com/coreos/go-oidc/v3/oidc"
)

var (
	ErrClosed = errors.New("authenticator has not been opened")
)

type OIDCAuthn struct {
	issuer   string
	clientID string

	ctx context.Context

	verifier *oidc.IDTokenVerifier
}

func NewOIDCAuthn(issuer string, clientID string) *OIDCAuthn {
	return &OIDCAuthn{
		issuer:   issuer,
		clientID: clientID,
	}
}

func (a *OIDCAuthn) Open(ctx context.Context) error {
	provider, err := oidc.NewProvider(ctx, a.issuer)
	if err != nil {
		return err
	}

	a.ctx = ctx
	a.verifier = provider.Verifier(&oidc.Config{ClientID: a.clientID})

	return nil
}

func (a *OIDCAuthn) Validate(token string) (string, error) {
	if a.verifier == nil {
		return "", ErrClosed
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
