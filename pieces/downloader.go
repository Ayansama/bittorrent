package pieces

import (
	"bittorrent/peer"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

const BlockSize = 16 * 1024 // 16 KB

func PieceLen(totalLength, pieceLength int64, pieceIndex int) int {
	start := int64(pieceIndex) * pieceLength
	end   := start + pieceLength
	if end > totalLength {
		end = totalLength
	}
	return int(end - start)
}

func DownloadPiece(conn net.Conn, pieceIdx, pieceLen int) ([]byte, error) {
	buf        := make([]byte, pieceLen)
	downloaded := 0
	choked     := false

	conn.SetDeadline(time.Now().Add(60 * time.Second))

	for downloaded < pieceLen {
		if choked {
			for {
				msg, err := peer.ReadMessage(conn)
				if err != nil {
					return nil, fmt.Errorf("read while choked: %w", err)
				}
				if msg.ID == peer.MsgUnchoke {
					choked = false
					break
				}
			}
		}

		blockLen := BlockSize
		if pieceLen-downloaded < blockLen {
			blockLen = pieceLen - downloaded
		}

		req := make([]byte, 12)
		binary.BigEndian.PutUint32(req[0:], uint32(pieceIdx))
		binary.BigEndian.PutUint32(req[4:], uint32(downloaded))
		binary.BigEndian.PutUint32(req[8:], uint32(blockLen))

		if err := peer.SendMessage(conn, peer.MsgRequest, req); err != nil {
			return nil, fmt.Errorf("send request failed: %w", err)
		}

		// Read until we get our block or get choked
		for {
			msg, err := peer.ReadMessage(conn)
			if err != nil {
				return nil, fmt.Errorf("read failed: %w", err)
			}

			switch msg.ID {
			case peer.MsgChoke:
				choked = true
				// Re-send interested to signal we still want data
				peer.SendMessage(conn, peer.MsgInterested, nil)
				goto nextBlock

			case peer.MsgPiece:
				if len(msg.Payload) < 8 {
					return nil, fmt.Errorf("piece payload too short")
				}
				begin := int(binary.BigEndian.Uint32(msg.Payload[4:8]))
				data  := msg.Payload[8:]
				copy(buf[begin:], data)
				downloaded += len(data)
				goto nextBlock

			case peer.MsgHave, peer.MsgUnchoke:
				// ignore, keep reading
			}
		}
		nextBlock:
	}
	return buf, nil
}

func Verify(data []byte, expected [20]byte) error {
	actual := sha1.Sum(data)
	if actual != expected {
		return fmt.Errorf("sha1 mismatch\n  got:  %x\n  want: %x", actual, expected)
	}
	return nil
}