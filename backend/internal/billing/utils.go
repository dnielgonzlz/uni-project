package billing

import (
	"encoding/json"
	"fmt"
)

// parseJSON is a helper used internally to unmarshal webhook payloads.
func parseJSON(data []byte, dest any) error {
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("billing: json unmarshal: %w", err)
	}
	return nil
}
