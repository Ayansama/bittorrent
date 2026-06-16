package storage

import (
	"encoding/json"
	"fmt"
	"os"
)

type ResumeState struct {
	Completed []int `json:"completed"`
}

func SaveResume(infoHash [20]byte, completed map[int]bool) error {
	path := fmt.Sprintf(".%x.resume", infoHash)
	var idxs []int
	for i := range completed {
		idxs = append(idxs, i)
	}
	data, err := json.Marshal(ResumeState{Completed: idxs})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func LoadResume(infoHash [20]byte) map[int]bool {
	path := fmt.Sprintf(".%x.resume", infoHash)
	data, err := os.ReadFile(path)
	if err != nil {
		return map[int]bool{}
	}
	var state ResumeState
	if err := json.Unmarshal(data, &state); err != nil {
		return map[int]bool{}
	}
	m := make(map[int]bool, len(state.Completed))
	for _, i := range state.Completed {
		m[i] = true
	}
	fmt.Printf("Resuming: %d pieces already done\n", len(m))
	return m
}