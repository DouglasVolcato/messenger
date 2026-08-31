package utils

import (
	"errors"
	"os"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

type UserInput struct {
	ID          string `json:"id"`
	Role        string `json:"role"`
	WorkspaceID string `json:"workspace_id"`
	CompanyID   string `json:"company_id"`
	CompanyRole string `json:"company_role"`
	SystemAdmin bool   `json:"system_admin"`
}

func GenerateJWT(user UserInput) (string, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return "", errors.New("JWT_SECRET is required")
	}

	claims := jwt.MapClaims{
		"user_id":      user.ID,
		"role":         user.Role,
		"workspace_id": user.WorkspaceID,
		"company_id":   user.CompanyID,
		"company_role": user.CompanyRole,
		"system_admin": user.SystemAdmin,
		"exp":          time.Now().Add(24 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func ValidateJWT(tokenString string) (*UserInput, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return nil, errors.New("JWT_SECRET is required")
	}

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return nil, errors.New("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid token claims")
	}
	userID, ok := claims["user_id"].(string)
	if !ok || userID == "" {
		return nil, errors.New("invalid user id")
	}

	user := &UserInput{ID: userID}
	if value, ok := claims["role"].(string); ok {
		user.Role = value
	}
	if value, ok := claims["workspace_id"].(string); ok {
		user.WorkspaceID = value
	}
	if value, ok := claims["company_id"].(string); ok {
		user.CompanyID = value
	}
	if value, ok := claims["company_role"].(string); ok {
		user.CompanyRole = value
	}
	if value, ok := claims["system_admin"].(bool); ok {
		user.SystemAdmin = value
	}
	return user, nil
}
