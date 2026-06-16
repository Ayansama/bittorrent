package storage

import (
	"fmt"
	"os"
	"sync"
)

type FileWriter struct {
	mu          sync.Mutex
	f           *os.File
	pieceLength int64
}

func NewFileWriter(name string, totalSize, pieceLength int64) (*FileWriter, error) {
	f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("open failed: %w", err)
	}

	// Pre-allocate the full file size
	if err := f.Truncate(totalSize); err != nil {
		f.Close()
		return nil, fmt.Errorf("truncate failed: %w", err)
	}

	return &FileWriter{f: f, pieceLength: pieceLength}, nil
}

func (fw *FileWriter) WritePiece(idx int, data []byte) error {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	offset := int64(idx) * fw.pieceLength
	_, err := fw.f.WriteAt(data, offset)
	return err
}

func (fw *FileWriter) Close() error {
	return fw.f.Close()
}