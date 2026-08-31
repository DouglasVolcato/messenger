package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/douglasvolcato/messager-architecture-challenge/internal/db"
	"github.com/douglasvolcato/messager-architecture-challenge/internal/models"
	"github.com/subosito/gotenv"
)

func main() {
	name := flag.String("name", "System Admin", "administrator name")
	username := flag.String("username", "admin", "administrator username")
	email := flag.String("email", "", "administrator email")
	password := flag.String("password", "", "administrator password")
	flag.Parse()

	if *email == "" || *password == "" {
		panic("email and password are required")
	}
	if err := gotenv.Load(); err != nil {
		panic(err)
	}
	if err := db.InitDB(); err != nil {
		panic(err)
	}
	if err := db.RunMigrations(); err != nil {
		panic(err)
	}

	ctx := context.Background()
	tx, err := db.BeginTransaction(ctx)
	if err != nil {
		panic(err)
	}
	user := models.User{
		Name:        *name,
		Username:    *username,
		Email:       *email,
		Password:    *password,
		Status:      "ACTIVE",
		SystemAdmin: true,
	}
	if err := user.Create(tx, ctx); err != nil {
		_ = db.RollbackTransaction(tx)
		panic(err)
	}
	if err := db.CommitTransaction(tx); err != nil {
		panic(err)
	}
	fmt.Printf("System administrator created: %s (%s)\n", user.Email, user.ID)
}
