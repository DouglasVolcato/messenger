package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func RunMigrations() error {
	migrationsDirectory := os.Getenv("MIGRATIONS_DIR")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	files, err := os.ReadDir(migrationsDirectory)
	if err != nil {
		return err
	}

	tx, err := DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS migrations (
			id VARCHAR(255) PRIMARY KEY,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		);
	`)
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		fileName := file.Name()

		var id string

		err := tx.QueryRowContext(
			ctx,
			"SELECT id FROM migrations WHERE id = $1",
			fileName,
		).Scan(&id)

		if err == nil {
			// Migração já executada
			continue
		}

		if err != sql.ErrNoRows {
			return err
		}

		filePath := filepath.Join(migrationsDirectory, fileName)

		fileContent, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}

		fmt.Printf("Applying migration: %s\n", fileName)

		_, err = tx.ExecContext(ctx, string(fileContent))
		if err != nil {
			return fmt.Errorf("error applying migration %s: %w", fileName, err)
		}

		_, err = tx.ExecContext(
			ctx,
			"INSERT INTO migrations (id) VALUES ($1)",
			fileName,
		)
		if err != nil {
			return err
		}

		fmt.Printf("Migration applied: %s\n", fileName)
	}

	return tx.Commit()
}
