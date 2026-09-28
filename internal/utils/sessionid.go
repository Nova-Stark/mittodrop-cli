package utils

import (
	"fmt"

	"github.com/google/uuid"
)

func GenerateSessionID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("sessionid: generate uuid v7: %w", err)
	}
	return id.String(), nil
}
