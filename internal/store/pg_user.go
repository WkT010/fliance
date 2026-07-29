package store

import (
	"database/sql"
	"fmt"

	"github.com/WkT010/nexa-exchange/internal/api"
)

type PGUserStore struct{ db *sql.DB }

func NewPGUserStore(db *sql.DB) *PGUserStore { return &PGUserStore{db: db} }

func (s *PGUserStore) GetByEmail(email string) (*api.User, error) {
	u := &api.User{}
	err := s.db.QueryRow(
		`SELECT id,email,password_hash,role,created_at,updated_at FROM users WHERE email=$1`, email).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get by email: %w", err)
	}
	return u, nil
}

func (s *PGUserStore) GetByID(id string) (*api.User, error) {
	u := &api.User{}
	err := s.db.QueryRow(
		`SELECT id,email,password_hash,role,created_at,updated_at FROM users WHERE id=$1`, id).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get by id: %w", err)
	}
	return u, nil
}

func (s *PGUserStore) Create(user *api.User) error {
	_, err := s.db.Exec(
		`INSERT INTO users (id,email,password_hash,role,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6)`,
		user.ID, user.Email, user.PasswordHash, user.Role, user.CreatedAt, user.UpdatedAt)
	return err
}

func (s *PGUserStore) Update(user *api.User) error {
	_, err := s.db.Exec(
		`UPDATE users SET password_hash=$1, updated_at=$2 WHERE id=$3`,
		user.PasswordHash, user.UpdatedAt, user.ID)
	return err
}
