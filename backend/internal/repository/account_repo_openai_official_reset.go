package repository

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"entgo.io/ent/dialect"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbaccount "github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.OpenAIOfficial7dResetRepository = (*accountRepository)(nil)

const (
	openAI7dResetMinimumDetectionDifference = 2 * time.Hour
	openAI7dResetPostHandledCooldown        = 2 * time.Hour
	openAI7dResetEvidenceVersion            = 1
	openAI7dResetWindow                     = 7 * 24 * time.Hour
	openAI7dResetBaselineEvidenceExtraKey   = "codex_official_7d_reset_baseline_evidence"
	openAI7dResetPendingEvidenceExtraKey    = "codex_official_7d_reset_pending_evidence"
)

func (r *accountRepository) ObserveOpenAI7dReset(
	ctx context.Context,
	accountID int64,
	observedAt, resetAt time.Time,
	windowDuration time.Duration,
	boundaryGrace time.Duration,
) (service.OpenAIOfficial7dResetObservation, error) {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return r.observeOpenAI7dReset(ctx, tx.Client(), accountID, observedAt, resetAt, windowDuration, boundaryGrace)
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return service.OpenAIOfficial7dResetObservation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	observation, err := r.observeOpenAI7dReset(txCtx, tx.Client(), accountID, observedAt, resetAt, windowDuration, boundaryGrace)
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
	windowDuration time.Duration,
	boundaryGrace time.Duration,
) (service.OpenAIOfficial7dResetObservation, error) {
	accountQuery := client.Account.Query().
		Where(dbaccount.IDEQ(accountID), dbaccount.DeletedAtIsNil())
	account, err := openAIAccountQueryForUpdate(client, accountQuery).Only(ctx)
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

	extra := copyJSONMap(account.Extra)
	if extra == nil {
		extra = make(map[string]any)
	}
	previousEvidence := parseOpenAIOfficial7dResetEvidence(extra[openAI7dResetBaselineEvidenceExtraKey])
	previousResetAt := account.Codex7dObservedResetAt
	if previousEvidence != nil {
		previous := previousEvidence.ResetAt
		previousResetAt = &previous
	} else if previousResetAt == nil {
		previousResetAt = openAI7dResetBaselineFromExtra(account.Extra)
	}

	identity, subscriptionActive := accountEntityToService(account).OpenAIChatGPTSubscriptionIdentity(observedAt)
	currentEvidence := service.OpenAIOfficial7dResetEvidence{
		Version:       openAI7dResetEvidenceVersion,
		ObservedAt:    observedAt,
		ResetAt:       resetAt,
		WindowSeconds: int64(windowDuration / time.Second),
	}
	if subscriptionActive {
		currentEvidence.PlanType = identity.PlanType
		currentEvidence.SubscriptionExpiresAt = copyTimePointer(identity.SubscriptionExpiresAt)
		extra[openAI7dResetBaselineEvidenceExtraKey] = currentEvidence
	} else {
		// Removing the trusted baseline makes a later newly purchased subscription
		// establish its own baseline before it can participate in detection.
		delete(extra, openAI7dResetBaselineEvidenceExtraKey)
	}

	detected := false
	if subscriptionActive &&
		openAIOfficial7dResetEvidenceIsCanonical(previousEvidence) &&
		openAIOfficial7dResetEvidenceIsCanonical(&currentEvidence) &&
		openAIOfficialSubscriptionIdentityEqual(previousEvidence, &currentEvidence) {
		_, detected, _ = classifyOpenAI7dResetObservation(
			&previousEvidence.ResetAt,
			account.CodexQuotaObservedAt,
			account.CodexOfficialEarlyResetHandledAt,
			account.CodexOfficialEarlyResetPending,
			observedAt,
			resetAt,
			boundaryGrace,
		)
	}
	if detected {
		eligibleAccountIDs, err := listEligibleOpenAIOfficial7dResetAccountIDs(ctx, client, observedAt)
		if err != nil {
			return service.OpenAIOfficial7dResetObservation{}, err
		}
		if !containsInt64(eligibleAccountIDs, accountID) {
			detected = false
		} else {
			pendingEvidence := currentEvidence
			pendingEvidence.EligibleAccountIDs = eligibleAccountIDs
			extra[openAI7dResetPendingEvidenceExtraKey] = pendingEvidence
		}
	}

	update := client.Account.UpdateOneID(accountID).
		SetCodex7dObservedResetAt(resetAt).
		SetCodexQuotaObservedAt(observedAt).
		SetExtra(extra)
	if detected {
		update.SetCodexOfficialEarlyResetPending(true).
			SetCodexOfficialEarlyResetDetectedAt(observedAt)
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
		return changed, false, false
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

func openAIOfficial7dResetEvidenceIsCanonical(evidence *service.OpenAIOfficial7dResetEvidence) bool {
	if evidence == nil {
		return false
	}
	planType := strings.ToLower(strings.TrimSpace(evidence.PlanType))
	return evidence.Version == openAI7dResetEvidenceVersion &&
		!evidence.ObservedAt.IsZero() &&
		!evidence.ResetAt.IsZero() &&
		evidence.WindowSeconds == int64(openAI7dResetWindow/time.Second) &&
		planType != "" && planType != "free" && planType != "abnormal"
}

func openAIOfficialSubscriptionIdentityEqual(left, right *service.OpenAIOfficial7dResetEvidence) bool {
	if left == nil || right == nil || left.PlanType != right.PlanType {
		return false
	}
	if left.SubscriptionExpiresAt == nil || right.SubscriptionExpiresAt == nil {
		return left.SubscriptionExpiresAt == nil && right.SubscriptionExpiresAt == nil
	}
	return left.SubscriptionExpiresAt.UTC().Truncate(time.Second).Equal(right.SubscriptionExpiresAt.UTC().Truncate(time.Second))
}

func parseOpenAIOfficial7dResetEvidence(value any) *service.OpenAIOfficial7dResetEvidence {
	if value == nil {
		return nil
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var evidence service.OpenAIOfficial7dResetEvidence
	if err := json.Unmarshal(payload, &evidence); err != nil || evidence.Version <= 0 {
		return nil
	}
	evidence.ObservedAt = evidence.ObservedAt.UTC().Truncate(time.Second)
	evidence.ResetAt = evidence.ResetAt.UTC().Truncate(time.Second)
	if evidence.SubscriptionExpiresAt != nil {
		expiresAt := evidence.SubscriptionExpiresAt.UTC().Truncate(time.Second)
		evidence.SubscriptionExpiresAt = &expiresAt
	}
	evidence.EligibleAccountIDs = normalizedInt64s(evidence.EligibleAccountIDs)
	return &evidence
}

func copyTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copyValue := value.UTC().Truncate(time.Second)
	return &copyValue
}

func containsInt64(values []int64, target int64) bool {
	index := sort.Search(len(values), func(i int) bool { return values[i] >= target })
	return index < len(values) && values[index] == target
}

func normalizedInt64s(values []int64) []int64 {
	unique := make(map[int64]struct{}, len(values))
	for _, value := range values {
		if value > 0 {
			unique[value] = struct{}{}
		}
	}
	normalized := make([]int64, 0, len(unique))
	for value := range unique {
		normalized = append(normalized, value)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i] < normalized[j] })
	return normalized
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
		query = openAIAccountQueryForUpdate(client, query)
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
			HandledAt:   copyTimePointer(account.CodexOfficialEarlyResetHandledAt),
			Evidence:    parseOpenAIOfficial7dResetEvidence(account.Extra[openAI7dResetPendingEvidenceExtraKey]),
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
		query = openAIAccountQueryForUpdate(client, query)
	}
	accounts, err := query.All(ctx)
	if err != nil {
		return nil, err
	}
	candidates := make([]service.OpenAIOfficial7dResetCandidate, 0, len(accounts))
	for _, account := range accounts {
		if _, active := accountEntityToService(account).OpenAIChatGPTSubscriptionIdentity(now); !active {
			continue
		}
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

func listEligibleOpenAIOfficial7dResetAccountIDs(
	ctx context.Context,
	client *dbent.Client,
	now time.Time,
) ([]int64, error) {
	accountQuery := client.Account.Query().
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
	accounts, err := accountQuery.All(ctx)
	if err != nil {
		return nil, err
	}
	accountIDs := make([]int64, 0, len(accounts))
	for _, account := range accounts {
		if _, active := accountEntityToService(account).OpenAIChatGPTSubscriptionIdentity(now); active {
			accountIDs = append(accountIDs, account.ID)
		}
	}
	return accountIDs, nil
}

func (r *accountRepository) MarkAllOpenAIOfficial7dResetsHandled(ctx context.Context, handledAt time.Time) error {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return markAllOpenAIOfficial7dResetsHandled(ctx, tx.Client(), handledAt)
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	if err := markAllOpenAIOfficial7dResetsHandled(txCtx, tx.Client(), handledAt); err != nil {
		return err
	}
	return tx.Commit()
}

func markAllOpenAIOfficial7dResetsHandled(ctx context.Context, client *dbent.Client, handledAt time.Time) error {
	accountQuery := client.Account.Query().
		Where(
			dbaccount.DeletedAtIsNil(),
			dbaccount.PlatformEQ(service.PlatformOpenAI),
			dbaccount.TypeEQ(service.AccountTypeOAuth),
			dbaccount.ParentAccountIDIsNil(),
		)
	accounts, err := openAIAccountQueryForUpdate(client, accountQuery).All(ctx)
	if err != nil {
		return err
	}
	handledAt = handledAt.UTC().Truncate(time.Second)
	for _, account := range accounts {
		extra := copyJSONMap(account.Extra)
		delete(extra, openAI7dResetPendingEvidenceExtraKey)
		if _, err := client.Account.UpdateOneID(account.ID).
			SetExtra(extra).
			SetCodexOfficialEarlyResetPending(false).
			ClearCodexOfficialEarlyResetDetectedAt().
			SetCodexOfficialEarlyResetHandledAt(handledAt).
			Save(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (r *accountRepository) MarkOpenAIOfficial7dResetRoundHandled(
	ctx context.Context,
	handledAt time.Time,
	events []service.OpenAIOfficial7dResetState,
) (int, error) {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return markOpenAIOfficial7dResetRoundHandled(ctx, tx.Client(), handledAt, events)
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	consumed, err := markOpenAIOfficial7dResetRoundHandled(txCtx, tx.Client(), handledAt, events)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return consumed, nil
}

func markOpenAIOfficial7dResetRoundHandled(
	ctx context.Context,
	client *dbent.Client,
	handledAt time.Time,
	events []service.OpenAIOfficial7dResetState,
) (int, error) {
	ordered := append([]service.OpenAIOfficial7dResetState(nil), events...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].AccountID < ordered[j].AccountID })
	seen := make(map[int64]struct{}, len(ordered))
	for _, event := range ordered {
		if event.AccountID <= 0 || event.DetectedAt.IsZero() {
			return 0, service.ErrOpenAIOfficialResetPendingChanged
		}
		if _, duplicate := seen[event.AccountID]; duplicate {
			return 0, service.ErrOpenAIOfficialResetPendingChanged
		}
		seen[event.AccountID] = struct{}{}
		accountQuery := client.Account.Query().Where(
			dbaccount.IDEQ(event.AccountID),
			dbaccount.DeletedAtIsNil(),
			dbaccount.PlatformEQ(service.PlatformOpenAI),
			dbaccount.TypeEQ(service.AccountTypeOAuth),
			dbaccount.ParentAccountIDIsNil(),
			dbaccount.CodexOfficialEarlyResetPendingEQ(true),
			dbaccount.CodexOfficialEarlyResetDetectedAtEQ(event.DetectedAt.UTC().Truncate(time.Second)),
		)
		account, err := openAIAccountQueryForUpdate(client, accountQuery).Only(ctx)
		if dbent.IsNotFound(err) {
			return 0, service.ErrOpenAIOfficialResetPendingChanged
		}
		if err != nil {
			return 0, err
		}
		extra := copyJSONMap(account.Extra)
		delete(extra, openAI7dResetPendingEvidenceExtraKey)
		if _, err := client.Account.UpdateOneID(account.ID).
			SetExtra(extra).
			SetCodexOfficialEarlyResetPending(false).
			ClearCodexOfficialEarlyResetDetectedAt().
			Save(ctx); err != nil {
			return 0, err
		}
	}
	if _, err := client.Account.Update().Where(
		dbaccount.DeletedAtIsNil(),
		dbaccount.PlatformEQ(service.PlatformOpenAI),
		dbaccount.TypeEQ(service.AccountTypeOAuth),
		dbaccount.ParentAccountIDIsNil(),
	).SetCodexOfficialEarlyResetHandledAt(handledAt.UTC().Truncate(time.Second)).Save(ctx); err != nil {
		return 0, err
	}
	return len(ordered), nil
}

func (r *accountRepository) ClearOpenAIOfficial7dResetPending(
	ctx context.Context,
	accountID int64,
	detectedAt time.Time,
) (bool, error) {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return clearOpenAIOfficial7dResetPending(ctx, tx.Client(), accountID, detectedAt)
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	cleared, err := clearOpenAIOfficial7dResetPending(txCtx, tx.Client(), accountID, detectedAt)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return cleared, nil
}

func clearOpenAIOfficial7dResetPending(
	ctx context.Context,
	client *dbent.Client,
	accountID int64,
	detectedAt time.Time,
) (bool, error) {
	accountQuery := client.Account.Query().Where(
		dbaccount.IDEQ(accountID),
		dbaccount.DeletedAtIsNil(),
		dbaccount.PlatformEQ(service.PlatformOpenAI),
		dbaccount.TypeEQ(service.AccountTypeOAuth),
		dbaccount.ParentAccountIDIsNil(),
		dbaccount.CodexOfficialEarlyResetPendingEQ(true),
		dbaccount.CodexOfficialEarlyResetDetectedAtEQ(detectedAt.UTC().Truncate(time.Second)),
	)
	account, err := openAIAccountQueryForUpdate(client, accountQuery).Only(ctx)
	if dbent.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	extra := copyJSONMap(account.Extra)
	delete(extra, openAI7dResetPendingEvidenceExtraKey)
	if _, err := client.Account.UpdateOneID(account.ID).
		SetExtra(extra).
		SetCodexOfficialEarlyResetPending(false).
		ClearCodexOfficialEarlyResetDetectedAt().
		Save(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func openAIAccountQueryForUpdate(client *dbent.Client, query *dbent.AccountQuery) *dbent.AccountQuery {
	if client != nil && client.Driver() != nil && client.Driver().Dialect() != dialect.SQLite {
		return query.ForUpdate()
	}
	return query
}
