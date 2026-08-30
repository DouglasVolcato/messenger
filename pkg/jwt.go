package utils

import (
	"errors"
	"os"

	jwt "github.com/golang-jwt/jwt/v5"
)

type UserInput struct {
	ID   string `json:"id"`
	Role string `json:"role"`
}

func GenerateJWT(user UserInput) (string, error) {
	claims := jwt.MapClaims{
		"user_id": user.ID,
		"role":    user.Role,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(os.Getenv("JWT_SECRET")))
}

func ValidateJWT(tokenString string) (*UserInput, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(os.Getenv("JWT_SECRET")), nil
	})
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		return &UserInput{
			ID:   claims["user_id"].(string),
			Role: claims["role"].(string),
		}, nil
	}
	return nil, errors.New("invalid token")
}
