package models

import (
	"context"
	"database/sql"

	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
)

type User struct {
	ID          string
	Name        string
	Email       string
	Password    string
	GlobalAdmin bool
}

func (u *User) Create(tx *sql.Tx, ctx context.Context) error {
	id, err := utils.GenerateUUID()
	if err != nil {
		return err
	}
	u.ID = id

	hashedPassword, err := utils.HashPassword(u.Password)
	if err != nil {
		return err
	}
	u.Password = hashedPassword

	_, err = tx.ExecContext(
		ctx,
		"insert into users (id,name,email,password,global_admin) values ($1,$2,$3,$4,$5)",
		u.ID, u.Name, u.Email, u.Password, u.GlobalAdmin,
	)
	return err
}

func (u *User) Update(tx *sql.Tx, ctx context.Context) error {
	_, err := tx.ExecContext(
		ctx,
		"update users set name = $1, email = $2 where id = $3",
		u.Name, u.Email, u.ID,
	)
	return err
}

func (u *User) UpdateGlobalAdmin(tx *sql.Tx, ctx context.Context) error {
	_, err := tx.ExecContext(
		ctx,
		"update users set global_admin = $1 where id = $2",
		u.GlobalAdmin, u.ID,
	)
	return err
}

func (u *User) UpdatePassword(tx *sql.Tx, ctx context.Context) error {
	hashedPassword, err := utils.HashPassword(u.Password)
	if err != nil {
		return err
	}
	u.Password = hashedPassword

	_, err = tx.ExecContext(
		ctx,
		"update users set password = $1 where id = $2",
		u.Password, u.ID,
	)
	return err
}

func (u *User) Delete(tx *sql.Tx, ctx context.Context) error {
	_, err := tx.ExecContext(ctx, "delete from users where id = $1", u.ID)
	return err
}

func (u *User) GetOne(db *sql.DB, ctx context.Context) error {
	result, err := db.QueryContext(
		ctx,
		"select id, name, email, global_admin from users where id = $1",
		u.ID,
	)
	if err != nil {
		return err
	}

	if result.Next() {
		err = result.Scan(&u.ID, &u.Name, &u.Email, &u.GlobalAdmin)
		if err != nil {
			return err
		}
	} else {
		return sql.ErrNoRows
	}
	return nil
}

func (u *User) GetOneByEmail(db *sql.DB, ctx context.Context) error {
	result, err := db.QueryContext(
		ctx,
		"select id, name, email, password, global_admin from users where email = $1",
		u.Email,
	)
	if err != nil {
		return err
	}
	defer result.Close()

	if result.Next() {
		err = result.Scan(&u.ID, &u.Name, &u.Email, &u.Password, &u.GlobalAdmin)
		if err != nil {
			return err
		}
	} else {
		return sql.ErrNoRows
	}
	return nil
}
