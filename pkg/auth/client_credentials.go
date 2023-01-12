package auth

import (
	"context"
	"errors"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

var (
	ErrEmptyOIDCClientSecret = errors.New("could not continue with empty OIDC client secret")
)

// Extended from https://github.com/pojntfx/goit/blob/main/pkg/token/token.go
type TokenManagerClientCredentials struct {
	oidcIssuer       string
	oidcClientID     string
	oidcClientSecret string

	tokenSource oauth2.TokenSource

	ctx context.Context
}

func NewTokenManagerClientCredentials(
	oidcIssuer string,
	oidcClientID string,
	oidcClientSecret string,

	ctx context.Context,
) *TokenManagerClientCredentials {
	return &TokenManagerClientCredentials{
		oidcIssuer:       oidcIssuer,
		oidcClientID:     oidcClientID,
		oidcClientSecret: oidcClientSecret,

		ctx: ctx,
	}
}

func (t *TokenManagerClientCredentials) InitialLogin() error {
	provider, err := oidc.NewProvider(t.ctx, t.oidcIssuer)
	if err != nil {
		return err
	}

	config := &oauth2.Config{
		ClientID:     t.oidcClientID,
		ClientSecret: t.oidcClientSecret,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID},
	}

	// TODO: Get OAuth2 token without redirect (with client secret)

	t.tokenSource = config.TokenSource(t.ctx, nil)

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

	return token.Extra("id_token").(string), nil
}
