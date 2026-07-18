package utils

import (
	"encoding/json"
	"errors"
)

var ErrInvalidJSON = errors.New("payload must be a valid JSON string")

// ValidateJSON checks if a string is syntactically valid JSON.
func ValidateJSON(str string) error {
	if str == "" {
		return nil
	}
	var js json.RawMessage
	if json.Unmarshal([]byte(str), &js) != nil {
		return ErrInvalidJSON
	}
	return nil
}
