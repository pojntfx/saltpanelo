package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

var (
	ErrEmptyOIDCIssuer      = errors.New("could not continue with empty OIDC issuer")
	ErrEmptyOIDCClientID    = errors.New("could not continue with empty OIDC client ID")
	ErrEmptyOIDCRedirectURL = errors.New("could not continue with empty OIDC redirect URL")

	ErrNotLoggedIn = errors.New("could not continue without being logged in")
)

// Extended from https://github.com/pojntfx/goit/blob/main/pkg/token/token.go
type TokenManager struct {
	oidcIssuer      string
	oidcClientID    string
	oidcRedirectURL string

	openURL func(string) error

	tokenSource oauth2.TokenSource

	ctx context.Context
}

func NewTokenManager(
	oidcIssuer string,
	oidcClientID string,
	oidcRedirectURL string,

	openURL func(string) error,

	ctx context.Context,
) *TokenManager {
	return &TokenManager{
		oidcIssuer:      oidcIssuer,
		oidcClientID:    oidcClientID,
		oidcRedirectURL: oidcRedirectURL,

		openURL: openURL,

		ctx: ctx,
	}
}

func (t *TokenManager) InitialLogin() error {
	provider, err := oidc.NewProvider(t.ctx, t.oidcIssuer)
	if err != nil {
		return err
	}

	config := &oauth2.Config{
		ClientID:    t.oidcClientID,
		RedirectURL: t.oidcRedirectURL,
		Endpoint:    provider.Endpoint(),
		Scopes:      []string{oidc.ScopeOpenID},
	}

	u, err := url.Parse(t.oidcRedirectURL)
	if err != nil {
		return err
	}

	srv := &http.Server{Addr: u.Host}

	errs := make(chan error)

	srv.Handler = http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/html")

		if _, err := fmt.Fprint(rw, `<!DOCTYPE html><script>window.open("", "_parent", "");window.close()</script>`); err != nil {
			errs <- err

			return
		}

		oauth2Token, err := config.Exchange(context.Background(), r.URL.Query().Get("code"))
		if err != nil {
			errs <- err

			return
		}

		t.tokenSource = config.TokenSource(t.ctx, oauth2Token)

		errs <- nil
	})

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			if err == http.ErrServerClosed {
				close(errs)

				return
			}

			errs <- err

			return
		}
	}()
	defer func() {
		if err := srv.Shutdown(t.ctx); err != nil {
			panic(err)
		}
	}()

	authURL := config.AuthCodeURL(t.oidcRedirectURL)
	if err := t.openURL(authURL); err != nil {
		return err
	}

	for err := range errs {
		return err
	}

	return nil
}

func (t *TokenManager) GetIDToken() (string, error) {
	if t.tokenSource == nil {
		return "", ErrNotLoggedIn
	}

	token, err := t.tokenSource.Token()
	if err != nil {
		return "", err
	}

	return token.Extra("id_token").(string), nil
}
