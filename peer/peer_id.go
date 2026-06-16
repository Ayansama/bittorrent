package peer

import (
	"crypto/rand"
	"fmt"
)

func GeneratePeerID() ([20]byte, error) {
	var id [20]byte
	copy(id[:], []byte("-GO0001-"))
	_, err := rand.Read(id[8:])
	if err != nil {
		return [20]byte{}, fmt.Errorf("failed to generate peer id: %w", err)
	}
	return id, nil
}