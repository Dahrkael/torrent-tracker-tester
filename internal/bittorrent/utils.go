package bittorrent

import (
	"fmt"
	"io"
	"net/http"
)

// readResponseBody reads the entire response body into a byte slice
func readResponseBody(resp *http.Response) ([]byte, error) {
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// parseCompactPeers converts the compact peer list into a list of Peer structs
func parseCompactPeers(peersData string) ([]Peer, error) {
	if len(peersData)%6 != 0 {
		return []Peer{}, fmt.Errorf("invalid peer format")
	}

	peers := []Peer{}
	data := []byte(peersData)
	for i := 0; i < len(data); i += 6 {
		ip := fmt.Sprintf("%d.%d.%d.%d", data[i], data[i+1], data[i+2], data[i+3])
		port := int(data[i+4])<<8 | int(data[i+5])
		peers = append(peers, Peer{IP: ip, Port: port})
	}

	return peers, nil
}
