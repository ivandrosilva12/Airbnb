package postgres

import (
	"context"

	"github.com/airhost/backend/internal/domain/identity"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// IdentityRepository is the Postgres implementation of identity.Repository. It
// runs against a querier (the pool, or a transaction inside a UnitOfWork).
type IdentityRepository struct {
	pool querier
}

// NewIdentityRepository builds an IdentityRepository.
func NewIdentityRepository(db querier) *IdentityRepository {
	return &IdentityRepository{pool: db}
}

const identityColumns = `id, user_id, status, document_type, document_ref, legal_name,
	rejection_reason, reviewer_id, reviewed_at, created_at, updated_at`

func (r *IdentityRepository) Create(ctx context.Context, v *identity.Verification) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO identity_verifications (`+identityColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		v.ID, v.UserID, string(v.Status), string(v.DocumentType), v.DocumentRef, v.LegalName,
		v.RejectionReason, nilUUID(v.ReviewerID), v.ReviewedAt, v.CreatedAt, v.UpdatedAt,
	)
	return mapError(err)
}

func (r *IdentityRepository) Update(ctx context.Context, v *identity.Verification) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE identity_verifications
		SET status=$2, document_type=$3, document_ref=$4, legal_name=$5,
			rejection_reason=$6, reviewer_id=$7, reviewed_at=$8, updated_at=$9
		WHERE id=$1`,
		v.ID, string(v.Status), string(v.DocumentType), v.DocumentRef, v.LegalName,
		v.RejectionReason, nilUUID(v.ReviewerID), v.ReviewedAt, v.UpdatedAt,
	)
	if err != nil {
		return mapError(err)
	}
	if ct.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func (r *IdentityRepository) FindByID(ctx context.Context, id uuid.UUID) (*identity.Verification, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+identityColumns+` FROM identity_verifications WHERE id=$1`, id)
	return scanVerification(row)
}

func (r *IdentityRepository) FindLatestByUser(ctx context.Context, userID uuid.UUID) (*identity.Verification, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+identityColumns+` FROM identity_verifications
		WHERE user_id=$1 ORDER BY created_at DESC LIMIT 1`, userID)
	return scanVerification(row)
}

func (r *IdentityRepository) ListByStatus(ctx context.Context, status identity.Status, page shared.Page) (shared.PageResult[*identity.Verification], error) {
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM identity_verifications WHERE status=$1`, string(status),
	).Scan(&total); err != nil {
		return shared.PageResult[*identity.Verification]{}, mapError(err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+identityColumns+` FROM identity_verifications WHERE status=$1
		ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		string(status), page.Limit, page.Offset,
	)
	if err != nil {
		return shared.PageResult[*identity.Verification]{}, mapError(err)
	}
	defer rows.Close()

	var items []*identity.Verification
	for rows.Next() {
		v, err := scanVerification(rows)
		if err != nil {
			return shared.PageResult[*identity.Verification]{}, err
		}
		items = append(items, v)
	}
	return shared.PageResult[*identity.Verification]{Items: items, Total: total}, mapError(rows.Err())
}

func scanVerification(row rowScanner) (*identity.Verification, error) {
	var (
		v          identity.Verification
		status     string
		docType    string
		reviewerID *uuid.UUID
	)
	if err := row.Scan(&v.ID, &v.UserID, &status, &docType, &v.DocumentRef, &v.LegalName,
		&v.RejectionReason, &reviewerID, &v.ReviewedAt, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, mapError(err)
	}
	v.Status = identity.Status(status)
	v.DocumentType = identity.DocumentType(docType)
	if reviewerID != nil {
		v.ReviewerID = *reviewerID
	}
	return &v, nil
}
