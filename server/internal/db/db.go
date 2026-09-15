package db

import (
	"context"
	"database/sql"
	"os"

	_ "github.com/lib/pq"
)

var DB *sql.DB

func InitDB() error {
	db, err := sql.Open("postgres", os.Getenv("DB_URL"))
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(0)
	err = db.Ping()
	if err != nil {
		return err
	}
	DB = db
	return nil
}

func CloseDB() error {
	if DB != nil {
		return DB.Close()
	}
	return nil
}

func BeginTransaction(ctx context.Context) (*sql.Tx, error) {
	tx, err := DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return tx, nil
}

func CommitTransaction(tx *sql.Tx) error {
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func RollbackTransaction(tx *sql.Tx) error {
	if err := tx.Rollback(); err != nil {
		return err
	}
	return nil
}
