// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package account

import (
	"context"
	"errors"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// ErrNotImplemented is returned by all stub methods.
var ErrNotImplemented = errors.New("not implemented")

// StubService is a Service that returns ErrNotImplemented for every call.
type StubService struct{}

func (StubService) Get(_ context.Context, _ domain.MerchantID) (domain.Account, error) {
	return domain.Account{}, ErrNotImplemented
}
func (StubService) Update(_ context.Context, _ domain.MerchantID, _ domain.AccountUpdate) (domain.Account, error) {
	return domain.Account{}, ErrNotImplemented
}
func (StubService) ListMembers(_ context.Context, _ domain.MerchantID, _ string, _ int) (domain.ListOf[domain.Member], error) {
	return domain.ListOf[domain.Member]{}, ErrNotImplemented
}
func (StubService) Create(_ context.Context, _ string, _ *domain.BusinessFacts, _ string) (domain.Account, error) {
	return domain.Account{}, ErrNotImplemented
}
func (StubService) List(_ context.Context, _ string, _ int) (domain.ListOf[domain.Account], error) {
	return domain.ListOf[domain.Account]{}, ErrNotImplemented
}
func (StubService) InviteMember(_ context.Context, _ domain.MerchantID, _ string, _ domain.MemberRole) (domain.Member, error) {
	return domain.Member{}, ErrNotImplemented
}
func (StubService) InviteTeamMember(_ context.Context, _ domain.MerchantID, _ string) (domain.Member, error) {
	return domain.Member{}, ErrNotImplemented
}
func (StubService) BindInvitedMember(_ context.Context, _ string, _ string) (domain.Member, domain.MerchantID, error) {
	return domain.Member{}, "", ErrNotImplemented
}

var _ Service = StubService{}
