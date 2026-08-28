package cerebra

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
)

// SessionAffinity manages model consistency within conversations
// Ensures the same model is reused throughout a session unless escalation is needed
type SessionAffinity struct {
	db     *sql.DB
	scorer *Scorer
}

// NewSessionAffinity creates a new session affinity manager
func NewSessionAffinity(db *sql.DB) *SessionAffinity {
	return &SessionAffinity{
		db:     db,
		scorer: NewScorer(),
	}
}

// CheckAffinity checks if a session already has a model and whether to reuse it
// Returns (sessionModel, shouldEscalate, error)
func (sa *SessionAffinity) CheckAffinity(ctx context.Context, params RouteParams) (string, bool, error) {
	var sessionModel sql.NullString
	var err error

	if params.IssueID != nil {
		sessionModel, err = sa.getIssueSessionModel(ctx, *params.IssueID)
	} else if params.ChatSessionID != nil {
		sessionModel, err = sa.getChatSessionModel(ctx, *params.ChatSessionID)
	} else {
		return "", false, nil
	}

	if err != nil {
		return "", false, err
	}

	// No existing session model
	if !sessionModel.Valid || sessionModel.String == "" {
		return "", false, nil
	}

	// Check if new prompt requires escalation
	newTier := sa.scorer.Score(params.Prompt)
	shouldEscalate := sa.shouldEscalate(sessionModel.String, newTier)

	return sessionModel.String, shouldEscalate, nil
}

// shouldEscalate determines if the new prompt complexity requires a stronger model
// Session affinity only escalates, never de-escalates
func (sa *SessionAffinity) shouldEscalate(sessionModel string, newTier Tier) bool {
	// Infer session model tier from name pattern
	sessionTier := inferTierFromModelName(sessionModel)

	// Tier hierarchy: simple < standard < heavy
	tierRank := map[Tier]int{
		TierSimple:   1,
		TierStandard: 2,
		TierHeavy:    3,
	}

	sessionRank := tierRank[sessionTier]
	newRank := tierRank[newTier]

	// Escalate if new tier is higher
	return newRank > sessionRank
}

// getIssueSessionModel retrieves the session_model for an issue
func (sa *SessionAffinity) getIssueSessionModel(ctx context.Context, issueID uuid.UUID) (sql.NullString, error) {
	query := `SELECT session_model FROM issues WHERE id = $1`
	var model sql.NullString
	err := sa.db.QueryRowContext(ctx, query, issueID).Scan(&model)
	if err != nil {
		return sql.NullString{}, err
	}
	return model, nil
}

// getChatSessionModel retrieves the session_model for a chat session
func (sa *SessionAffinity) getChatSessionModel(ctx context.Context, chatSessionID uuid.UUID) (sql.NullString, error) {
	query := `SELECT session_model FROM chat_sessions WHERE id = $1`
	var model sql.NullString
	err := sa.db.QueryRowContext(ctx, query, chatSessionID).Scan(&model)
	if err != nil {
		return sql.NullString{}, err
	}
	return model, nil
}

// SetIssueSessionModel sets the session_model for an issue
func (sa *SessionAffinity) SetIssueSessionModel(ctx context.Context, issueID uuid.UUID, model string) error {
	query := `UPDATE issues SET session_model = $2 WHERE id = $1`
	_, err := sa.db.ExecContext(ctx, query, issueID, model)
	return err
}

// SetChatSessionModel sets the session_model for a chat session
func (sa *SessionAffinity) SetChatSessionModel(ctx context.Context, chatSessionID uuid.UUID, model string) error {
	query := `UPDATE chat_sessions SET session_model = $2 WHERE id = $1`
	_, err := sa.db.ExecContext(ctx, query, chatSessionID, model)
	return err
}

// ClearIssueSessionModel clears the session_model for an issue (e.g., when issue is closed)
func (sa *SessionAffinity) ClearIssueSessionModel(ctx context.Context, issueID uuid.UUID) error {
	query := `UPDATE issues SET session_model = NULL WHERE id = $1`
	_, err := sa.db.ExecContext(ctx, query, issueID)
	return err
}

// ClearChatSessionModel clears the session_model for a chat session
func (sa *SessionAffinity) ClearChatSessionModel(ctx context.Context, chatSessionID uuid.UUID) error {
	query := `UPDATE chat_sessions SET session_model = NULL WHERE id = $1`
	_, err := sa.db.ExecContext(ctx, query, chatSessionID)
	return err
}

// inferTierFromModelName is duplicated here to avoid circular dependency with router
func inferTierFromModelName(modelID string) Tier {
	lower := modelID
	if len(lower) > 0 {
		lower = modelID
	}

	simplePatterns := []string{"mini", "haiku", "flash", "nano", "small", "lite"}
	for _, pattern := range simplePatterns {
		if contains(lower, pattern) {
			return TierSimple
		}
	}

	heavyPatterns := []string{"opus", "large", "ultra", "max", "plus", "pro"}
	for _, pattern := range heavyPatterns {
		if contains(lower, pattern) {
			return TierHeavy
		}
	}

	return TierStandard
}

// contains checks if s contains substr (case-insensitive)
func contains(s, substr string) bool {
	// Simple case-insensitive substring check
	sLower := ""
	subLower := ""
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			sLower += string(r + 32)
		} else {
			sLower += string(r)
		}
	}
	for _, r := range substr {
		if r >= 'A' && r <= 'Z' {
			subLower += string(r + 32)
		} else {
			subLower += string(r)
		}
	}
	
	if len(subLower) == 0 {
		return true
	}
	if len(sLower) < len(subLower) {
		return false
	}
	
	for i := 0; i <= len(sLower)-len(subLower); i++ {
		if sLower[i:i+len(subLower)] == subLower {
			return true
		}
	}
	return false
}
