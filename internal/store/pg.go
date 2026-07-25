package store

import (
	"database/sql"
	"fmt"
	_ "github.com/lib/pq"
)

func NewPG(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil { return nil, fmt.Errorf("open: %w", err) }
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	if err := db.Ping(); err != nil { return nil, fmt.Errorf("ping: %w", err) }
	return db, nil
}