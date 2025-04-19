package bittorrent

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// HTTPTracker implements the Tracker interface for HTTP trackers
type HTTPTracker struct {
	trackerURL string
	config     ClientConfig
	client     *http.Client
	compact    bool // Determines if a compact peer list is requested
}

// NewHTTPTracker creates a new HTTP tracker with the provided configuration and tracker URL
func NewHTTPTracker(trackerURL string, config ClientConfig, timeout time.Duration) *HTTPTracker {
	return &HTTPTracker{
		trackerURL: trackerURL,
		config:     config,
		client: &http.Client{
			Timeout: timeout,
		},
		compact: true, // Default to requesting compact peer list
	}
}

// Announce for HTTPTracker sends a request to the HTTP tracker
func (ht *HTTPTracker) Announce(infoHash string, stats TransferStats) ([]Peer, error) {
	hashBytes, err := hex.DecodeString(infoHash)
	if err != nil || len(hashBytes) != 20 {
		return nil, fmt.Errorf("invalid info_hash: must be a 40-character hexadecimal hash")
	}

	params := url.Values{}
	// info_hash: The 20-byte SHA-1 hash of the torrent's info dictionary
	params.Set("info_hash", infoHash)
	// peer_id: A unique 20-byte identifier for this client
	params.Set("peer_id", ht.config.PeerID)
	// port: The port this client is listening on
	params.Set("port", fmt.Sprintf("%d", ht.config.Address.Port))
	// uploaded: Total bytes uploaded so far
	params.Set("uploaded", fmt.Sprintf("%d", stats.Uploaded))
	// downloaded: Total bytes downloaded so far
	params.Set("downloaded", fmt.Sprintf("%d", stats.Downloaded))
	// left: Bytes remaining to download (estimated total size)
	params.Set("left", fmt.Sprintf("%d", stats.Left))
	if ht.compact {
		// compact: Request a compact peer list (1 = yes, 0 = no)
		params.Set("compact", "1") // Request compact peer list if compact is true
	} else {
		params.Set("compact", "0") // Request non-compact peer list if compact is false
	}
	// event: Indicates the client's state (e.g., "started", "stopped", "completed")
	//params.Set("event", "")

	reqURL := fmt.Sprintf("%s?%s", ht.trackerURL, params.Encode())
	resp, err := ht.client.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("error contacting HTTP tracker: %v", err)
	}
	defer resp.Body.Close()

	// Read the response body
	bodyBytes, err := readResponseBody(resp)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected tracker response: %s", resp.Status)
	}

	// Decode the bencode response into a TrackerResponse
	var trackerResp TrackerResponse
	err = UnmarshalBencode(bodyBytes, &trackerResp)
	if err != nil {
		return nil, fmt.Errorf("error decoding response: %v", err)
	}

	// Check if the tracker sent a failure reason
	if trackerResp.FailureReason != "" {
		return nil, fmt.Errorf("failure reason: %v", err)
	}

	// Handle peers based on compact or non-compact format
	if ht.compact {
		peersData, ok := trackerResp.Peers.(string)
		if !ok {
			return nil, fmt.Errorf("expected compact peers as string")
		}
		return parseCompactPeers(peersData)
	}

	// Non-compact case: expect a list of peer dictionaries
	peerList, ok := trackerResp.Peers.([]interface{})
	if !ok {
		return nil, fmt.Errorf("expected non-compact peers as list")
	}
	return parseNonCompactPeers(peerList)
}

// parseNonCompactPeers converts a non-compact peer list into a list of Peer structs
func parseNonCompactPeers(peerList []interface{}) ([]Peer, error) {
	var peers []Peer
	for _, item := range peerList {
		peerDict, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid non-compact peer format")
		}

		// Marshal and unmarshal to NonCompactPeer struct
		data, err := MarshalBencode(peerDict)
		if err != nil {
			return nil, fmt.Errorf("error marshaling peer dict: %v", err)
		}
		var ncPeer NonCompactPeer
		if err := UnmarshalBencode(data, &ncPeer); err != nil {
			return nil, fmt.Errorf("error unmarshaling peer dict: %v", err)
		}
		peers = append(peers, Peer{IP: ncPeer.IP, Port: ncPeer.Port})
	}
	return peers, nil
}
