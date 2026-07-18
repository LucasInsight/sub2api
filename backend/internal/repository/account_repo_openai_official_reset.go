package repository

import (
	"context"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbaccount "github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.OpenAIOfficial7dResetRepository = (*accountRepository)(nil)

func (r *accountRepository) ObserveOpenAI7dReset(
	ctx context.Context,
	accountID int64,
	observedAt, resetAt time.Time,
	boundaryGrace time.Duration,
) (bool, error) {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return r.observeOpenAI7dReset(ctx, tx.Client(), accountID, observedAt, resetAt, boundaryGrace)
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	detected, err := r.observeOpenAI7dReset(txCtx, tx.Client(), accountID, observedAt, resetAt, boundaryGrace)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return detected, nil
}

func (r *accountRepository) observeOpenAI7dReset(
	ctx context.Context,
	client *dbent.Client,
	accountID int64,
	observedAt, resetAt time.Time,
	boundaryGrace time.Duration,
) (bool, error) {
	account, err := client.Account.Query().
		Where(dbaccount.IDEQ(accountID), dbaccount.DeletedAtIsNil()).
		ForUpdate().
		Only(ctx)
	if err != nil {
		return false, translatePersistenceError(err, service.ErrAccountNotFound, nil)
	}
	if account.Platform != service.PlatformOpenAI || account.Type != service.AccountTypeOAuth || account.ParentAccountID != nil {
		return false, nil
	}

	observedAt = observedAt.UTC().Truncate(time.Second)
	resetAt = resetAt.UTC().Truncate(time.Second)
	_, detected := classifyOpenAI7dResetObservation(
		account.Codex7dObservedResetAt,
		observedAt,
		resetAt,
		boundaryGrace,
	)

	update := client.Account.UpdateOneID(accountID).
		SetCodex7dObservedResetAt(resetAt).
		SetCodexQuotaObservedAt(observedAt)
	if detected {
		update.SetCodexOfficialEarlyResetPending(true).
			SetCodexOfficialEarlyResetDetectedAt(observedAt)
	}
	if _, err := update.Save(ctx); err != nil {
		return false, translatePersistenceError(err, service.ErrAccountNotFound, nil)
	}
	return detected, nil
}

func classifyOpenAI7dResetObservation(
	previousResetAt *time.Time,
	observedAt, resetAt time.Time,
	boundaryGrace time.Duration,
) (changed, detected bool) {
	changed = previousResetAt != nil && !previousResetAt.Equal(resetAt)
	detected = changed && observedAt.Add(boundaryGrace).Before(*previousResetAt)
	return changed, detected
}

func (r *accountRepository) ListPendingOpenAIOfficial7dResets(ctx context.Context) ([]service.OpenAIOfficial7dResetState, error) {
	client := clientFromContext(ctx, r.client)
	query := client.Account.Query().Where(
		dbaccount.DeletedAtIsNil(),
		dbaccount.PlatformEQ(service.PlatformOpenAI),
		dbaccount.TypeEQ(service.AccountTypeOAuth),
		dbaccount.ParentAccountIDIsNil(),
		dbaccount.CodexOfficialEarlyResetPendingEQ(true),
	)
	if dbent.TxFromContext(ctx) != nil {
		query = query.ForUpdate()
	}
	accounts, err := query.All(ctx)
	if err != nil {
		return nil, err
	}
	states := make([]service.OpenAIOfficial7dResetState, 0, len(accounts))
	for _, account := range accounts {
		if account.CodexOfficialEarlyResetDetectedAt == nil {
			continue
		}
		states = append(states, service.OpenAIOfficial7dResetState{
			AccountID:  account.ID,
			DetectedAt: *account.CodexOfficialEarlyResetDetectedAt,
		})
	}
	return states, nil
}

func (r *accountRepository) MarkOpenAIOfficial7dResetsHandled(ctx context.Context, accountIDs []int64, handledAt time.Time) error {
	if len(accountIDs) == 0 {
		return nil
	}
	client := clientFromContext(ctx, r.client)
	_, err := client.Account.Update().
		Where(
			dbaccount.IDIn(accountIDs...),
			dbaccount.CodexOfficialEarlyResetPendingEQ(true),
		).
		SetCodexOfficialEarlyResetPending(false).
		SetCodexOfficialEarlyResetHandledAt(handledAt.UTC().Truncate(time.Second)).
		Save(ctx)
	return err
}
