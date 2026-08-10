package store

import (
	"context"
	"database/sql"

	"github.com/WkT010/nexa-exchange/internal/audit"
)

// PGAuditStore persists admin audit entries in PostgreSQL (table audit_logs,
// created by migration 009). It implements audit.AuditStore and is wired into
// audit.Logger by cmd/api-gateway.
type PGAuditStore struct {
	db *sql.DB
}

func NewPGAuditStore(db *sql.DB) *PGAuditStore { return &PGAuditStore{db: db} }

// Record inserts one audit entry. The id and ts columns are assigned by the
// database (BIGSERIAL / DEFAULT now()).
func (s *PGAuditStore) Record(ctx context.Context, e audit.Entry) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_logs
			(actor_user_id, actor_email, action, target_type, target_id, ip, user_agent, details, success, error)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		e.ActorUserID, e.ActorEmail, e.Action, e.TargetType, e.TargetID,
		e.IPAddress, e.UserAgent, e.Details, e.Success, e.ErrorMsg)
	return err
}
