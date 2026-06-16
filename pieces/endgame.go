package pieces

import (
	"bittorrent/peer"
	"bittorrent/tracker"
	"fmt"
	"net"
	"time"
)

type PieceWriter interface {
	WritePiece(int, []byte) error
}

func RunEndgame(allPeers []tracker.PeerAddr, pm *PieceManager,
	hashes [][20]byte, pieceLength, totalLength int64,
	fw PieceWriter, infoHash, peerID [20]byte) {

	remaining := pm.Remaining()
	if len(remaining) == 0 {
		return
	}

	fmt.Printf("\n  endgame — %d pieces left\n", len(remaining))

	for _, idx := range remaining {
		if pm.IsCompleted(idx) {
			continue
		}

		fmt.Printf("  endgame: trying piece %d...\n", idx)
		downloaded := false

		limit := len(allPeers)
		if limit > 20 {
			limit = 20
		}

		for i := 0; i < limit; i++ {
			addr := allPeers[i].AddrString()

			conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
			if err != nil {
				continue
			}

			_, err = peer.DoHandshake(conn, infoHash, peerID)
			if err != nil {
				conn.Close()
				continue
			}

			conn.SetDeadline(time.Now().Add(15 * time.Second))
			peer.SendMessage(conn, peer.MsgInterested, nil)

			unchoked := false
			for !unchoked {
				msg, err := peer.ReadMessage(conn)
				if err != nil {
					break
				}
				switch msg.ID {
				case peer.MsgBitfield:
					peer.SendMessage(conn, peer.MsgInterested, nil)
				case peer.MsgUnchoke:
					unchoked = true
				}
			}

			if !unchoked {
				conn.Close()
				continue
			}

			conn.SetDeadline(time.Now().Add(30 * time.Second))
			pl := PieceLen(totalLength, pieceLength, idx)
			data, err := DownloadPiece(conn, idx, pl)
			conn.Close()

			if err != nil {
				continue
			}
			if err := Verify(data, hashes[idx]); err != nil {
				continue
			}

			fw.WritePiece(idx, data)
			pm.Complete(idx)
			fmt.Printf("  endgame: piece %d ✓\n", idx)
			downloaded = true
			break
		}

		if !downloaded {
			fmt.Printf("  endgame: piece %d not available\n", idx)
		}
	}
}