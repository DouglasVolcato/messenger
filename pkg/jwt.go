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
}

func GenerateJWT(user UserInput) (string, error) {
	claims := jwt.MapClaims{
		"user_id":      user.ID,
		"role":         user.Role,
		"workspace_id": user.WorkspaceID,
		"company_id":   user.CompanyID,
		"company_role": user.CompanyRole,
		"exp":          time.Now().Add(24 * time.Hour).Unix(),
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
	return user, nil
}
