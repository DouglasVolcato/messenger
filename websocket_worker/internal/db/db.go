package db

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/lib/pq"
)

var DB *sql.DB

func Init() error {
	url := os.Getenv("DB_URL")
	if url == "" {
		return fmt.Errorf("DB_URL is required")
	}

	database, err := sql.Open("postgres", url)
	if err != nil {
		return err
	}
	database.SetMaxOpenConns(8)
	database.SetMaxIdleConns(4)
	database.SetConnMaxIdleTime(2 * time.Minute)
	database.SetConnMaxLifetime(15 * time.Minute)

	if err := database.Ping(); err != nil {
		_ = database.Close()
		return err
	}
	DB = database
	return nil
}

func Close() error {
	if DB != nil {
		return DB.Close()
	}
	return nil
}
