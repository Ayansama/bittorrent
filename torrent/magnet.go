package torrent

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

type MagnetLink struct {
	InfoHash    [20]byte
	DisplayName string
	Trackers    []string
}

func ParseMagnet(uri string) (*MagnetLink, error) {
	if !strings.HasPrefix(uri, "magnet:?") {
		return nil, fmt.Errorf("not a magnet link")
	}

	// Parse query string after "magnet:?"
	query, err := url.ParseQuery(uri[8:])
	if err != nil {
		return nil, fmt.Errorf("failed to parse magnet: %w", err)
	}

	// Extract xt (exact topic) — contains the info hash
	xt := query.Get("xt")
	if xt == "" {
		return nil, fmt.Errorf("missing xt parameter")
	}

	// xt format: urn:btih:<hash>
	// Hash can be 40-char hex or 32-char base32
	if !strings.HasPrefix(xt, "urn:btih:") {
		return nil, fmt.Errorf("unsupported xt format: %s", xt)
	}

	hashStr := xt[9:]
	infoHash, err := parseInfoHash(hashStr)
	if err != nil {
		return nil, fmt.Errorf("invalid info hash: %w", err)
	}

	m := &MagnetLink{
		InfoHash:    infoHash,
		DisplayName: query.Get("dn"),
	}

	// Extract trackers (tr parameter, can appear multiple times)
	for _, tr := range query["tr"] {
		decoded, err := url.QueryUnescape(tr)
		if err == nil {
			m.Trackers = append(m.Trackers, decoded)
		}
	}

	return m, nil
}

func parseInfoHash(s string) ([20]byte, error) {
	var hash [20]byte

	switch len(s) {
	case 40:
		// Hex encoded
		b, err := hex.DecodeString(s)
		if err != nil {
			return hash, err
		}
		copy(hash[:], b)

	case 32:
		// Base32 encoded
		b, err := base32Decode(s)
		if err != nil {
			return hash, err
		}
		copy(hash[:], b)

	default:
		return hash, fmt.Errorf("unexpected hash length %d", len(s))
	}

	return hash, nil
}

// base32Decode decodes a standard base32 string (no padding)
func base32Decode(s string) ([]byte, error) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	s = strings.ToUpper(s)

	// Add padding
	for len(s)%8 != 0 {
		s += "="
	}

	result := make([]byte, 0, len(s)*5/8)
	buf := 0
	bits := 0

	for _, c := range s {
		if c == '=' {
			break
		}
		idx := strings.IndexRune(alphabet, c)
		if idx < 0 {
			return nil, fmt.Errorf("invalid base32 char: %c", c)
		}
		buf = (buf << 5) | idx
		bits += 5
		if bits >= 8 {
			bits -= 8
			result = append(result, byte(buf>>bits))
			buf &= (1 << bits) - 1
		}
	}
	return result, nil
}