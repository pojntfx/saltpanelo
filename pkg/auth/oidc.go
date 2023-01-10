package auth

import "errors"

var (
	ErrEmptyOIDCIssuer      = errors.New("could not continue with empty OIDC issuer")
	ErrEmptyOIDCClientID    = errors.New("could not continue with empty OIDC client ID")
	ErrEmptyOIDCRedirectURL = errors.New("could not continue with empty OIDC redirect URL")

	// TODO: Drop after lifecycle is implemented
	ErrEmptyOIDCAPITokenURL = errors.New("could not continue with empty OIDC API token")
)
