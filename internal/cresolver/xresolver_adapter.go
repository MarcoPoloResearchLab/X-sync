package cresolver

import (
	"context"
	"errors"
	"strings"

	"github.com/f-sync/fsync/internal/handles"
	"github.com/f-sync/fsync/internal/xresolver"
)

const (
	errMessageNilXResolverService = "xresolver service is nil"
	errMessageEmptyProfileResults = "xresolver returned no profile results"
)

// xResolverAccountAdapter converts xresolver.Service profiles into AccountRecord values.
type xResolverAccountAdapter struct {
	service *xresolver.Service
}

// NewAccountResolverFromXResolver constructs an AccountResolver backed by an xresolver.Service.
func NewAccountResolverFromXResolver(service *xresolver.Service) (AccountResolver, error) {
	if service == nil {
		return nil, errors.New(errMessageNilXResolverService)
	}
	return &xResolverAccountAdapter{service: service}, nil
}

// ResolveAccount resolves the supplied account identifier using the wrapped xresolver service.
func (adapter *xResolverAccountAdapter) ResolveAccount(ctx context.Context, accountID string) (handles.AccountRecord, error) {
	normalizedAccountID := strings.TrimSpace(accountID)
	accountRecord := handles.AccountRecord{AccountID: normalizedAccountID}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return accountRecord, ctxErr
	}

	profiles := adapter.service.ResolveBatch(ctx, xresolver.Request{IDs: []string{normalizedAccountID}})
	if len(profiles) == 0 {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return accountRecord, ctxErr
		}
		return accountRecord, errors.New(errMessageEmptyProfileResults)
	}

	profile := profiles[0]
	if trimmedProfileID := strings.TrimSpace(profile.ID); trimmedProfileID != "" {
		accountRecord.AccountID = trimmedProfileID
	}

	trimmedHandle := strings.TrimSpace(profile.Handle)
	if trimmedHandle != "" {
		accountRecord.UserName = trimmedHandle
	}

	trimmedDisplayName := strings.TrimSpace(profile.DisplayName)
	if trimmedDisplayName != "" {
		accountRecord.DisplayName = trimmedDisplayName
	}

	trimmedErrorMessage := strings.TrimSpace(profile.Err)
	if trimmedErrorMessage != "" {
		return accountRecord, errors.New(trimmedErrorMessage)
	}

	return accountRecord, nil
}
