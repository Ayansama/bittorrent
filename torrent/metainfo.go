package torrent

import (
	"crypto/sha1"
	"fmt"
	"os"
)

// Add this field to TorrentMeta struct
type TorrentMeta struct {
    Announce     string
    AnnounceList []string  // ← add this
    InfoHash     [20]byte
    PieceLength  int64
    Pieces       [][20]byte
    Name         string
    Length       int64
    Files        []FileInfo
}

type FileInfo struct {
	Length int64
	Path   []string
}

func ParseTorrent(path string) (*TorrentMeta, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	decoded, _, err := Decode(raw, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to decode torrent: %w", err)
	}

	meta, ok := decoded.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid torrent structure")
	}

	info, ok := meta["info"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("missing info dict")
	}

	// Compute info_hash from raw bytes
	infoHash, err := computeInfoHash(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to compute info_hash: %w", err)
	}

	// Parse piece hashes
	rawPieces, ok := info["pieces"].([]byte)
	if !ok || len(rawPieces)%20 != 0 {
		return nil, fmt.Errorf("invalid pieces field")
	}
	pieces := make([][20]byte, len(rawPieces)/20)
	for i := range pieces {
		copy(pieces[i][:], rawPieces[i*20:(i+1)*20])
	}

	t := &TorrentMeta{
		InfoHash:    infoHash,
		PieceLength: info["piece length"].(int64),
		Pieces:      pieces,
		Name:        string(info["name"].([]byte)),
	}

	// Announce URL
	if ann, ok := meta["announce"].([]byte); ok {
		t.Announce = string(ann)
	}
	// Extract announce-list (list of lists of strings)
if announceList, ok := meta["announce-list"].([]interface{}); ok {
    seen := make(map[string]bool)
    seen[t.Announce] = true // don't duplicate primary announce
    for _, tier := range announceList {
        if tierList, ok := tier.([]interface{}); ok {
            for _, u := range tierList {
                if url, ok := u.([]byte); ok {
                    s := string(url)
                    if !seen[s] {
                        seen[s] = true
                        t.AnnounceList = append(t.AnnounceList, s)
                    }
                }
            }
        }
    }
}

	// Single-file vs multi-file
	if length, ok := info["length"].(int64); ok {
		t.Length = length
	} else if files, ok := info["files"].([]interface{}); ok {
		for _, f := range files {
			fmap := f.(map[string]interface{})
			fi := FileInfo{Length: fmap["length"].(int64)}
			for _, p := range fmap["path"].([]interface{}) {
				fi.Path = append(fi.Path, string(p.([]byte)))
			}
			t.Files = append(t.Files, fi)
			t.Length += fi.Length
		}
	}

	return t, nil
}

// computeInfoHash finds the raw "info" dict bytes in the torrent
// file and SHA1 hashes them.
func computeInfoHash(raw []byte) ([20]byte, error) {
	// Find "4:info" key in the raw bytes
	key := []byte("4:info")
	idx := -1
	for i := 0; i <= len(raw)-len(key); i++ {
		match := true
		for j, b := range key {
			if raw[i+j] != b {
				match = false
				break
			}
		}
		if match {
			idx = i + len(key)
			break
		}
	}
	if idx == -1 {
		return [20]byte{}, fmt.Errorf("info key not found")
	}

	// Parse the info value to find where it ends
	_, end, err := Decode(raw, idx)
	if err != nil {
		return [20]byte{}, err
	}

	return sha1.Sum(raw[idx:end]), nil
}