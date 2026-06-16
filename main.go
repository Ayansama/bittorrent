package main

import (
	"bittorrent/dht"
	"bittorrent/peer"
	"bittorrent/pieces"
	"bittorrent/storage"
	"bittorrent/torrent"
	"bittorrent/tracker"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

func main() {
	var (
		meta *torrent.TorrentMeta
		err  error
	)

	// Determine input
	input := ""
	if len(os.Args) > 1 {
		input = os.Args[1]
	} else {
		fmt.Println("BitTorrent Downloader")
		fmt.Println("─────────────────────")
		fmt.Println("Paste a magnet link or enter a .torrent filename:")
		fmt.Print("> ")
		fmt.Scanln(&input)
	}

	if strings.HasPrefix(input, "magnet:?") {
		fmt.Println("\nParsing magnet link...")
		magnet, err := torrent.ParseMagnet(input)
		if err != nil {
			log.Fatal("Invalid magnet link:", err)
		}
		fmt.Printf("InfoHash:  %x\n", magnet.InfoHash)
		fmt.Printf("Name:      %s\n", magnet.DisplayName)
		fmt.Printf("Trackers:  %d in magnet\n", len(magnet.Trackers))

		meta = &torrent.TorrentMeta{
			InfoHash:     magnet.InfoHash,
			Name:         magnet.DisplayName,
			AnnounceList: magnet.Trackers,
		}
		if len(magnet.Trackers) > 0 {
			meta.Announce = magnet.Trackers[0]
		}
	} else {
		torrentFile := input
		if torrentFile == "" {
			torrentFile = "ubuntu.torrent"
		}
		meta, err = torrent.ParseTorrent(torrentFile)
		if err != nil {
			log.Fatal(err)
		}
	}

	// peerID declared here — used by everything below
	peerID, err := peer.GeneratePeerID()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\nName:      %s\n", meta.Name)
	fmt.Printf("InfoHash:  %x\n", meta.InfoHash)
	if meta.PieceLength > 0 {
		fmt.Printf("Pieces:    %d x %d KB\n", len(meta.Pieces), meta.PieceLength/1024)
		fmt.Printf("Size:      %.2f GB\n\n", float64(meta.Length)/1024/1024/1024)
	}

	// Build tracker list
	trackers := []string{}
	if meta.Announce != "" {
		trackers = append(trackers, meta.Announce)
	}
	trackers = append(trackers, meta.AnnounceList...)
	trackers = append(trackers,
		"udp://tracker.opentrackr.org:1337/announce",
		"udp://open.stealth.si:80/announce",
		"udp://tracker.torrent.eu.org:451/announce",
		"udp://exodus.desync.com:6969/announce",
		"udp://tracker.cyberia.is:6969/announce",
	)

	// Deduplicate trackers
	seenTrackers := make(map[string]bool)
	var uniqueTrackers []string
	for _, t := range trackers {
		if !seenTrackers[t] {
			seenTrackers[t] = true
			uniqueTrackers = append(uniqueTrackers, t)
		}
	}

	// Collect peers from all trackers
	seen := make(map[string]bool)
	var allPeers []tracker.PeerAddr

	for _, t := range uniqueTrackers {
		var peers []tracker.PeerAddr
		var err error
		if strings.HasPrefix(t, "udp://") {
			peers, err = tracker.UDPGetPeers(t, meta.InfoHash, peerID, meta.Length)
		} else {
			peers, err = httpGetPeers(t, meta, peerID)
		}
		if err != nil {
			continue
		}
		for _, p := range peers {
			key := fmt.Sprintf("%s:%d", p.IP, p.Port)
			if !seen[key] {
				seen[key] = true
				allPeers = append(allPeers, p)
			}
		}
	}

	fmt.Printf("Found %d unique peers\n\n", len(allPeers))
	if len(allPeers) == 0 {
		log.Fatal("No peers found")
	}

	// Magnet links need piece metadata before downloading
if meta.PieceLength == 0 {
    fmt.Println("\nFetching metadata via DHT...")
    fullMeta, err := dht.FetchMagnetMetadata(meta.InfoHash, peerID, allPeers)
    if err != nil {
        log.Fatal("Failed to fetch metadata:", err)
    }
    meta = fullMeta
    fmt.Printf("Got metadata — %d pieces x %d KB\n",
        len(meta.Pieces), meta.PieceLength/1024)
}

	// Set up piece manager and file writer
	pm := pieces.NewPieceManager(meta.Pieces, meta.PieceLength, meta.Length)
	savedProgress := storage.LoadResume(meta.InfoHash)
	pm.LoadCompleted(savedProgress)

	fw, err := storage.NewFileWriter(meta.Name, meta.Length, meta.PieceLength)
	if err != nil {
		log.Fatal("file writer:", err)
	}
	defer fw.Close()

	fmt.Printf("Downloading: %s\n", meta.Name)
	fmt.Println("Starting workers...")
	fmt.Println()

	start := time.Now()

	// Feed peers into channel
	peerCh := make(chan tracker.PeerAddr, len(allPeers))
	for _, p := range allPeers {
		peerCh <- p
	}
	close(peerCh)

	// Launch worker pool
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range peerCh {
				if pm.IsDone() {
					return
				}
				runWorker(p, meta, peerID, pm, fw)
			}
		}()
	}

	// Progress reporter
	go func() {
		for !pm.IsDone() {
			time.Sleep(3 * time.Second)
			done, total := pm.Stats()
			elapsed := time.Since(start)
			speed := float64(done) * float64(meta.PieceLength) /
				elapsed.Seconds() / 1024 / 1024
			fmt.Printf("\r  %.1f%% (%d/%d pieces) — %.2f MB/s       ",
				float64(done)/float64(total)*100, done, total, speed)
		}
	}()

	// Save progress every 30 seconds
	go func() {
		for !pm.IsDone() {
			time.Sleep(30 * time.Second)
			storage.SaveResume(meta.InfoHash, pm.Snapshot())
		}
	}()

	wg.Wait()

	// Endgame — finish remaining pieces with fresh connections
	if !pm.IsDone() {
		pieces.RunEndgame(allPeers, pm, meta.Pieces,
			meta.PieceLength, meta.Length, fw,
			meta.InfoHash, peerID)
	}

	storage.SaveResume(meta.InfoHash, pm.Snapshot())

	done, total := pm.Stats()
	fmt.Println()
	if done == total {
		fmt.Printf("\nDownload complete! %.2f GB in %v\n",
			float64(meta.Length)/1024/1024/1024,
			time.Since(start).Round(time.Second))
	} else {
		fmt.Printf("\nStopped: %d/%d pieces (%.1f%%)\n",
			done, total, float64(done)/float64(total)*100)
	}
}

func runWorker(p tracker.PeerAddr, meta *torrent.TorrentMeta,
	peerID [20]byte, pm *pieces.PieceManager, fw *storage.FileWriter) {

	addr := p.AddrString()

	conn, _, err := peer.ConnectAndHandshake(addr, meta.InfoHash, peerID)
	if err != nil {
		return
	}
	defer conn.Close()

	peer.SendMessage(conn, peer.MsgInterested, nil)

	conn.SetDeadline(time.Now().Add(30 * time.Second))
	unchoked := false
	for !unchoked {
		msg, err := peer.ReadMessage(conn)
		if err != nil {
			return
		}
		switch msg.ID {
		case peer.MsgBitfield:
			peer.SendMessage(conn, peer.MsgInterested, nil)
		case peer.MsgUnchoke:
			unchoked = true
		case peer.MsgChoke:
			peer.SendMessage(conn, peer.MsgInterested, nil)
		}
	}

	for {
		if pm.IsDone() {
			return
		}

		idx, ok := pm.NextPiece()
		if !ok {
			return
		}

		conn.SetDeadline(time.Now().Add(60 * time.Second))
		pl := pieces.PieceLen(meta.Length, meta.PieceLength, idx)

		data, err := pieces.DownloadPiece(conn, idx, pl)
		if err != nil {
			pm.Fail(idx)
			return
		}
		if err := pieces.Verify(data, meta.Pieces[idx]); err != nil {
			pm.Fail(idx)
			return
		}
		if err := fw.WritePiece(idx, data); err != nil {
			pm.Fail(idx)
			return
		}
		pm.Complete(idx)
	}
}

func httpGetPeers(trackerURL string, meta *torrent.TorrentMeta,
	peerID [20]byte) ([]tracker.PeerAddr, error) {

	params := url.Values{}
	params.Set("info_hash", string(meta.InfoHash[:]))
	params.Set("peer_id", string(peerID[:]))
	params.Set("port", "6881")
	params.Set("uploaded", "0")
	params.Set("downloaded", "0")
	params.Set("left", strconv.FormatInt(meta.Length, 10))
	params.Set("compact", "1")
	params.Set("event", "started")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(trackerURL + "?" + params.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	decoded, _, err := torrent.Decode(body, 0)
	if err != nil {
		return nil, err
	}

	dict, ok := decoded.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid response")
	}
	if reason, ok := dict["failure reason"]; ok {
		return nil, fmt.Errorf("%s", string(reason.([]byte)))
	}

	rawPeers, ok := dict["peers"].([]byte)
	if !ok {
		return nil, fmt.Errorf("no peers field")
	}

	var peers []tracker.PeerAddr
	for i := 0; i < len(rawPeers); i += 6 {
		ip   := net.IP(rawPeers[i : i+4]).String()
		port := binary.BigEndian.Uint16(rawPeers[i+4 : i+6])
		peers = append(peers, tracker.PeerAddr{IP: ip, Port: port})
	}
	return peers, nil
}