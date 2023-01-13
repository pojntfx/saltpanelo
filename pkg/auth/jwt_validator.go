package auth

import (
	"context"
	"net/url"
	"time"

	"github.com/auth0/go-jwt-middleware/v2/jwks"
	"github.com/auth0/go-jwt-middleware/v2/validator"
)

type JWTAuthn struct {
	issuer   string
	clientID string
	audience string

	ctx context.Context

	verifier *validator.Validator
}

func NewJWTAuthn(issuer string, clientID string, audience string) *JWTAuthn {
	return &JWTAuthn{
		issuer:   issuer,
		clientID: clientID,
		audience: audience,
	}
}

func (a *JWTAuthn) Open(ctx context.Context) error {
	u, err := url.Parse(a.issuer)
	if err != nil {
		return err
	}

	provider := jwks.NewCachingProvider(u, time.Minute*5)

	verifier, err := validator.New(
		provider.KeyFunc,
		validator.RS256,
		u.String(),
		[]string{a.audience},
	)
	if err != nil {
		return err
	}

	a.ctx = ctx
	a.verifier = verifier

	return nil
}

func (a *JWTAuthn) Validate(token string) error {
	if a.verifier == nil {
		return ErrClosed
	}

	if _, err := a.verifier.ValidateToken(a.ctx, token); err != nil {
		return err
	}

	return nil
}
