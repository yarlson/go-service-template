package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

type Verifier struct {
	verifier *oidc.IDTokenVerifier
}

func NewVerifier(ctx context.Context, issuerURL, audience string) (*Verifier, error) {
	httpClient := &http.Client{Timeout: 10 * time.Second}
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, httpClient), issuerURL)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider: %w", err)
	}

	return &Verifier{
		verifier: provider.Verifier(&oidc.Config{ClientID: audience}),
	}, nil
}

func (v *Verifier) Verify(ctx context.Context, rawToken string) (string, error) {
	token, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		return "", fmt.Errorf("verify token: %w", err)
	}
	if token.Subject == "" {
		return "", errors.New("token subject is empty")
	}
	return token.Subject, nil
}
