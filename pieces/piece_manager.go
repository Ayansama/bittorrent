package pieces

import (
	"fmt"
	"sync"
)

type PieceManager struct {
	mu          sync.Mutex
	total       int
	Pieces      [][20]byte
	PieceLength int64
	TotalLength int64
	completed   map[int]bool
	inFlight    map[int]bool
	failures    map[int]int
}

func NewPieceManager(hashes [][20]byte, pieceLength, totalLength int64) *PieceManager {
	return &PieceManager{
		total:       len(hashes),
		Pieces:      hashes,
		PieceLength: pieceLength,
		TotalLength: totalLength,
		completed:   make(map[int]bool),
		inFlight:    make(map[int]bool),
		failures:    make(map[int]int),
	}
}

func (pm *PieceManager) NextPiece() (int, bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for i := 0; i < pm.total; i++ {
		if !pm.completed[i] && !pm.inFlight[i] {
			pm.inFlight[i] = true
			return i, true
		}
	}
	return 0, false
}

func (pm *PieceManager) Complete(idx int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.inFlight, idx)
	pm.completed[idx] = true
	done := len(pm.completed)
	pct  := float64(done) / float64(pm.total) * 100
	fmt.Printf("\r  progress: %.1f%% (%d/%d pieces)", pct, done, pm.total)
}

func (pm *PieceManager) Fail(idx int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.inFlight, idx)
	pm.failures[idx]++
}

func (pm *PieceManager) IsDone() bool {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return len(pm.completed) == pm.total
}

func (pm *PieceManager) Stats() (int, int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return len(pm.completed), pm.total
}

func (pm *PieceManager) LoadCompleted(completed map[int]bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for idx := range completed {
		pm.completed[idx] = true
	}
}

func (pm *PieceManager) Snapshot() map[int]bool {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	c := make(map[int]bool, len(pm.completed))
	for k, v := range pm.completed {
		c[k] = v
	}
	return c
}

func (pm *PieceManager) IsEndgame() bool {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	remaining := pm.total - len(pm.completed) - len(pm.inFlight)
	return remaining <= 3 && remaining > 0
}

func (pm *PieceManager) Remaining() []int {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	var r []int
	for i := 0; i < pm.total; i++ {
		if !pm.completed[i] {
			r = append(r, i)
		}
	}
	return r
}

func (pm *PieceManager) IsCompleted(idx int) bool {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.completed[idx]
}