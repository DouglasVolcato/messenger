package utils

import (
	"os"

	"golang.org/x/crypto/bcrypt"
)

func HashPassword(password string) (string, error) {
	saltedPassword := password + os.Getenv("PASSWORD_SALT")
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(saltedPassword), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashedPassword), nil
}

func ComparePassword(hashedPassword string, password string) bool {
	saltedPassword := password + os.Getenv("PASSWORD_SALT")
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(saltedPassword))
	return err == nil
}
