package repository

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbaccount "github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.OpenAIOfficial7dResetRepository = (*accountRepository)(nil)

const (
	openAI7dResetMinimumDetectionDifference = 2 * time.Hour
	openAI7dResetPostHandledCooldown        = 2 * time.Hour
)

func (r *accountRepository) ObserveOpenAI7dReset(
	ctx context.Context,
	accountID int64,
	observedAt, resetAt time.Time,
	boundaryGrace time.Duration,
) (service.OpenAIOfficial7dResetObservation, error) {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return r.observeOpenAI7dReset(ctx, tx.Client(), accountID, observedAt, resetAt, boundaryGrace)
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return service.OpenAIOfficial7dResetObservation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	observation, err := r.observeOpenAI7dReset(txCtx, tx.Client(), accountID, observedAt, resetAt, boundaryGrace)
	if err != nil {
		return service.OpenAIOfficial7dResetObservation{}, err
	}
	if err := tx.Commit(); err != nil {
		return service.OpenAIOfficial7dResetObservation{}, err
	}
	return observation, nil
}

func (r *accountRepository) observeOpenAI7dReset(
	ctx context.Context,
	client *dbent.Client,
	accountID int64,
	observedAt, resetAt time.Time,
	boundaryGrace time.Duration,
) (service.OpenAIOfficial7dResetObservation, error) {
	account, err := client.Account.Query().
		Where(dbaccount.IDEQ(accountID), dbaccount.DeletedAtIsNil()).
		ForUpdate().
		Only(ctx)
	if err != nil {
		return service.OpenAIOfficial7dResetObservation{}, translatePersistenceError(err, service.ErrAccountNotFound, nil)
	}
	if account.Platform != service.PlatformOpenAI || account.Type != service.AccountTypeOAuth || account.ParentAccountID != nil {
		return service.OpenAIOfficial7dResetObservation{}, nil
	}

	observedAt = observedAt.UTC().Truncate(time.Second)
	resetAt = resetAt.UTC().Truncate(time.Second)
	if openAI7dResetObservationIsStale(account.CodexQuotaObservedAt, observedAt) {
		return service.OpenAIOfficial7dResetObservation{}, nil
	}
	previousResetAt := account.Codex7dObservedResetAt
	if previousResetAt == nil {
		previousResetAt = openAI7dResetBaselineFromExtra(account.Extra)
	}
	_, detected, clearPending := classifyOpenAI7dResetObservation(
		previousResetAt,
		account.CodexQuotaObservedAt,
		account.CodexOfficialEarlyResetHandledAt,
		account.CodexOfficialEarlyResetPending,
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
	} else if clearPending {
		// Once the previous official boundary has naturally elapsed, stale
		// early-reset evidence no longer belongs to the active window.
		update.SetCodexOfficialEarlyResetPending(false)
	}
	if _, err := update.Save(ctx); err != nil {
		return service.OpenAIOfficial7dResetObservation{}, translatePersistenceError(err, service.ErrAccountNotFound, nil)
	}
	observation := service.OpenAIOfficial7dResetObservation{
		Detected:   detected,
		ResetAt:    resetAt,
		ObservedAt: observedAt,
	}
	if previousResetAt != nil {
		previous := *previousResetAt
		observation.PreviousResetAt = &previous
	}
	return observation, nil
}

func openAI7dResetObservationIsStale(previousObservedAt *time.Time, observedAt time.Time) bool {
	if previousObservedAt == nil {
		return false
	}
	previous := previousObservedAt.UTC().Truncate(time.Second)
	return !observedAt.After(previous)
}

func classifyOpenAI7dResetObservation(
	previousResetAt *time.Time,
	previousObservedAt, handledAt *time.Time,
	pending bool,
	observedAt, resetAt time.Time,
	boundaryGrace time.Duration,
) (changed, detected, clearPending bool) {
	changed = previousResetAt != nil && !previousResetAt.Equal(resetAt)
	if !changed {
		return changed, false, false
	}
	if !observedAt.Add(boundaryGrace).Before(*previousResetAt) {
		return changed, false, pending
	}
	if resetAt.Sub(*previousResetAt).Abs() < openAI7dResetMinimumDetectionDifference {
		return changed, false, false
	}
	if handledAt != nil {
		handled := handledAt.UTC().Truncate(time.Second)
		if observedAt.Before(handled.Add(openAI7dResetPostHandledCooldown)) {
			return changed, false, false
		}
		if previousObservedAt == nil || !previousObservedAt.After(handled) {
			// This account did not establish a new baseline after the most recent
			// global reset. Its changed window is therefore a late observation of
			// the event already covered by handledAt, not a new reset request.
			return changed, false, false
		}
	}
	return changed, !pending, false
}

func openAI7dResetBaselineFromExtra(extra map[string]any) *time.Time {
	for _, key := range []string{"codex_7d_reset_at", "codex_7d_quota_estimate_period_key"} {
		if value, ok := parseOpenAI7dResetBaseline(extra[key]); ok {
			value = value.UTC().Truncate(time.Second)
			return &value
		}
	}
	return nil
}

func parseOpenAI7dResetBaseline(value any) (time.Time, bool) {
	switch typed := value.(type) {
	case time.Time:
		return typed, !typed.IsZero()
	case *time.Time:
		if typed != nil && !typed.IsZero() {
			return *typed, true
		}
	case string:
		parsed, err := time.Parse(time.RFC3339, typed)
		return parsed, err == nil
	case int64:
		return time.Unix(typed, 0), typed > 0
	case int:
		return time.Unix(int64(typed), 0), typed > 0
	case float64:
		return time.Unix(int64(typed), 0), typed > 0
	case json.Number:
		seconds, err := typed.Int64()
		return time.Unix(seconds, 0), err == nil && seconds > 0
	}
	return time.Time{}, false
}

func (r *accountRepository) ListPendingOpenAIOfficial7dResets(ctx context.Context) ([]service.OpenAIOfficial7dResetState, error) {
	client := clientFromContext(ctx, r.client)
	query := client.Account.Query().Where(
		dbaccount.DeletedAtIsNil(),
		dbaccount.PlatformEQ(service.PlatformOpenAI),
		dbaccount.TypeEQ(service.AccountTypeOAuth),
		dbaccount.ParentAccountIDIsNil(),
		dbaccount.CodexOfficialEarlyResetPendingEQ(true),
	).Order(dbent.Asc(dbaccount.FieldID))
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
			AccountID:   account.ID,
			AccountName: account.Name,
			DetectedAt:  *account.CodexOfficialEarlyResetDetectedAt,
		})
	}
	return states, nil
}

func (r *accountRepository) ListEligibleOpenAIOfficial7dResetCandidates(ctx context.Context, now time.Time) ([]service.OpenAIOfficial7dResetCandidate, error) {
	client := clientFromContext(ctx, r.client)
	query := client.Account.Query().
		Where(
			dbaccount.DeletedAtIsNil(),
			dbaccount.PlatformEQ(service.PlatformOpenAI),
			dbaccount.TypeEQ(service.AccountTypeOAuth),
			dbaccount.ParentAccountIDIsNil(),
			dbaccount.StatusEQ(service.StatusActive),
			dbaccount.SchedulableEQ(true),
			dbaccount.Or(dbaccount.ExpiresAtIsNil(), dbaccount.ExpiresAtGT(now)),
		).
		Order(dbent.Asc(dbaccount.FieldID))
	if dbent.TxFromContext(ctx) != nil {
		query = query.ForUpdate()
	}
	accounts, err := query.All(ctx)
	if err != nil {
		return nil, err
	}
	candidates := make([]service.OpenAIOfficial7dResetCandidate, 0, len(accounts))
	for _, account := range accounts {
		candidates = append(candidates, service.OpenAIOfficial7dResetCandidate{
			AccountID:       account.ID,
			Pending:         account.CodexOfficialEarlyResetPending,
			DetectedAt:      account.CodexOfficialEarlyResetDetectedAt,
			QuotaObservedAt: account.CodexQuotaObservedAt,
			HandledAt:       account.CodexOfficialEarlyResetHandledAt,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.Pending != right.Pending {
			return left.Pending
		}
		if left.QuotaObservedAt == nil || right.QuotaObservedAt == nil {
			return left.QuotaObservedAt == nil && right.QuotaObservedAt != nil
		}
		if !left.QuotaObservedAt.Equal(*right.QuotaObservedAt) {
			return left.QuotaObservedAt.Before(*right.QuotaObservedAt)
		}
		return left.AccountID < right.AccountID
	})
	return candidates, nil
}

func (r *accountRepository) MarkAllOpenAIOfficial7dResetsHandled(ctx context.Context, handledAt time.Time) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.Account.Update().
		Where(
			dbaccount.DeletedAtIsNil(),
			dbaccount.PlatformEQ(service.PlatformOpenAI),
			dbaccount.TypeEQ(service.AccountTypeOAuth),
			dbaccount.ParentAccountIDIsNil(),
		).
		SetCodexOfficialEarlyResetPending(false).
		SetCodexOfficialEarlyResetHandledAt(handledAt.UTC().Truncate(time.Second)).
		Save(ctx)
	return err
}

func (r *accountRepository) ClearOpenAIOfficial7dResetPending(
	ctx context.Context,
	accountID int64,
	detectedAt time.Time,
) (bool, error) {
	client := clientFromContext(ctx, r.client)
	updated, err := client.Account.Update().
		Where(
			dbaccount.IDEQ(accountID),
			dbaccount.DeletedAtIsNil(),
			dbaccount.PlatformEQ(service.PlatformOpenAI),
			dbaccount.TypeEQ(service.AccountTypeOAuth),
			dbaccount.ParentAccountIDIsNil(),
			dbaccount.CodexOfficialEarlyResetPendingEQ(true),
			dbaccount.CodexOfficialEarlyResetDetectedAtEQ(detectedAt.UTC().Truncate(time.Second)),
		).
		SetCodexOfficialEarlyResetPending(false).
		ClearCodexOfficialEarlyResetDetectedAt().
		Save(ctx)
	if err != nil {
		return false, err
	}
	return updated == 1, nil
}
