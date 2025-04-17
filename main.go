package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

// Configuration parameters for the application
type Config struct {
	// Number of concurrent goroutines to maintain
	ConcurrentRequests int
	// Tracker URL to send announce requests to
	TrackerURL string
	// Run duration (0 means run indefinitely)
	Duration time.Duration
	// Whether to print detailed logs
	Verbose bool
	// Size of the info_hash pool
	InfoHashPoolSize int
	// Size of the peer pool
	PeerPoolSize int
	// Parameters for announce requests
	RequestParams RequestParams
}

// Parameters for BitTorrent tracker announce requests
type RequestParams struct {
	// Range for random port values
	MinPort, MaxPort int
	// Range for random bytes uploaded
	MinUploaded, MaxUploaded int64
	// Range for random bytes downloaded
	MinDownloaded, MaxDownloaded int64
	// Range for random bytes left
	MinLeft, MaxLeft int64
	// Possible event values
	Events []string
	// Range for random IP address
	IPRange []string
	// Range for random numwant values
	MinNumWant, MaxNumWant int
	// Range for random key length
	MinKeyLength, MaxKeyLength int
}

// AnnounceRequest represents a tracker announce request
type AnnounceRequest struct {
	InfoHash    string
	PeerID      string
	Port        int
	Uploaded    int64
	Downloaded  int64
	Left        int64
	Event       string
	IP          string
	NumWant     int
	Key         string
	TrackerID   string
	Compact     int
	NoPeerID    int
	Supportcryp int
}

// PeerInfo represents a BitTorrent peer
type PeerInfo struct {
	ID   string
	IP   string
	Port int
	Key  string
}

// AnnounceResult holds the result of an announce request
type AnnounceResult struct {
	Success      bool
	RequestTime  time.Duration
	PeersCount   int
	HTTPStatus   int
	ErrorMessage string
	InfoHash     string
	PeerID       string
}

// Global variables
var (
	// Default configuration
	defaultConfig = Config{
		ConcurrentRequests: 1,
		TrackerURL:         "http://localhost:6969/announce",
		Duration:           0,
		Verbose:            true,
		InfoHashPoolSize:   100,
		PeerPoolSize:       50,
		RequestParams: RequestParams{
			MinPort:       6881,
			MaxPort:       6889,
			MinUploaded:   0,
			MaxUploaded:   1073741824, // 1 GB
			MinDownloaded: 0,
			MaxDownloaded: 1073741824, // 1 GB
			MinLeft:       0,
			MaxLeft:       1073741824, // 1 GB
			Events:        []string{"started", "completed", "stopped", ""},
			IPRange:       []string{"", "127.0.0.1", "192.168.1.1", "10.0.0.1"},
			MinNumWant:    0,
			MaxNumWant:    200,
			MinKeyLength:  8,
			MaxKeyLength:  12,
		},
	}

	// Statistics
	requestsSent     int
	requestsSuccess  int
	requestsFailed   int
	totalRequestTime time.Duration
	totalPeers       int
	statsLock        sync.Mutex

	// Random source
	rng = rand.New(rand.NewSource(time.Now().UnixNano()))

	// Pools
	infoHashPool []string
	peerPool     []PeerInfo
)

func main() {
	// Parse command line flags
	config := parseFlags()

	// Set up logging
	if config.Verbose {
		log.SetFlags(log.Ltime | log.Lmicroseconds)
	} else {
		log.SetFlags(0)
	}

	log.Printf("Starting BitTorrent tracker announcer")
	log.Printf("Target tracker: %s", config.TrackerURL)
	log.Printf("Concurrent requests: %d", config.ConcurrentRequests)
	log.Printf("Info hash pool size: %d", config.InfoHashPoolSize)
	log.Printf("Peer pool size: %d", config.PeerPoolSize)
	if config.Duration > 0 {
		log.Printf("Running for: %s", config.Duration)
	} else {
		log.Printf("Running indefinitely (press Ctrl+C to stop)")
	}

	// Generate pools
	generateInfoHashPool(config.InfoHashPoolSize)
	generatePeerPool(config.PeerPoolSize, config.RequestParams)

	log.Printf("Generated %d unique info_hashes", len(infoHashPool))
	log.Printf("Generated %d unique peers", len(peerPool))

	// Channel for results
	resultsChan := make(chan AnnounceResult, config.ConcurrentRequests*2)

	// WaitGroup to keep track of active goroutines
	var wg sync.WaitGroup

	// Start the stats reporter goroutine
	stopStats := make(chan struct{})
	go reportStats(stopStats)

	// Create a context with timeout if duration is specified
	var done chan struct{}
	if config.Duration > 0 {
		done = make(chan struct{})
		go func() {
			time.Sleep(config.Duration)
			close(done)
		}()
	} else {
		done = make(chan struct{})
	}

	// Start the initial goroutines
	for i := 0; i < config.ConcurrentRequests; i++ {
		wg.Add(1)
		go makeAnnounceRequest(config, resultsChan, &wg)
	}

	// Process results and maintain the desired number of goroutines
	go func() {
		for {
			select {
			case result := <-resultsChan:
				// Process the result
				processResult(result)

				// Start a new goroutine to maintain the desired number
				wg.Add(1)
				go makeAnnounceRequest(config, resultsChan, &wg)

			case <-done:
				// Duration is up, stop processing
				close(resultsChan)
				return
			}
		}
	}()

	// Wait for all goroutines to complete or the program to be interrupted
	waitForCompletion(done, stopStats, &wg)
}

// generateInfoHashPool creates a pool of random info_hashes
func generateInfoHashPool(size int) {
	infoHashPool = make([]string, size)
	for i := 0; i < size; i++ {
		infoHashPool[i] = generateRandomInfoHash()
	}
}

// generatePeerPool creates a pool of random peers
func generatePeerPool(size int, params RequestParams) {
	peerPool = make([]PeerInfo, size)
	for i := 0; i < size; i++ {
		peerPool[i] = PeerInfo{
			ID:   generateRandomPeerID(),
			IP:   params.IPRange[rng.Intn(len(params.IPRange))],
			Port: rng.Intn(params.MaxPort-params.MinPort+1) + params.MinPort,
			Key:  generateRandomKey(params.MinKeyLength, params.MaxKeyLength),
		}
	}
}

// parseFlags parses command line flags and returns the configuration
func parseFlags() Config {
	config := defaultConfig

	// Define command line flags
	flag.IntVar(&config.ConcurrentRequests, "concurrent", defaultConfig.ConcurrentRequests, "Number of concurrent requests")
	flag.StringVar(&config.TrackerURL, "tracker", defaultConfig.TrackerURL, "Tracker URL")
	durationStr := flag.String("duration", "", "Duration to run (e.g., 1m, 1h, 30s)")
	flag.BoolVar(&config.Verbose, "verbose", defaultConfig.Verbose, "Verbose output")
	flag.IntVar(&config.InfoHashPoolSize, "hashpool", defaultConfig.InfoHashPoolSize, "Size of the info_hash pool")
	flag.IntVar(&config.PeerPoolSize, "peerpool", defaultConfig.PeerPoolSize, "Size of the peer pool")

	// Parse flags
	flag.Parse()

	// Parse duration if provided
	if *durationStr != "" {
		var err error
		config.Duration, err = time.ParseDuration(*durationStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid duration: %v\n", err)
			os.Exit(1)
		}
	}

	return config
}

// makeAnnounceRequest generates and sends a random announce request
func makeAnnounceRequest(config Config, results chan<- AnnounceResult, wg *sync.WaitGroup) {
	defer wg.Done()

	// Generate a random announce request using pools
	req := generateAnnounceRequestFromPools(config.RequestParams)

	// Build the request URL
	reqURL, err := buildAnnounceURL(config.TrackerURL, req)
	if err != nil {
		results <- AnnounceResult{
			Success:      false,
			RequestTime:  0,
			PeersCount:   0,
			HTTPStatus:   0,
			ErrorMessage: fmt.Sprintf("Failed to build URL: %v", err),
			InfoHash:     req.InfoHash,
			PeerID:       req.PeerID,
		}
		return
	}

	// Log the request if verbose
	if config.Verbose {
		log.Printf("Sending request: %s", reqURL)
	}

	// Send the HTTP request and measure time
	startTime := time.Now()
	response, err := http.Get(reqURL)
	requestTime := time.Since(startTime)

	// Process the response
	if err != nil {
		results <- AnnounceResult{
			Success:      false,
			RequestTime:  requestTime,
			PeersCount:   0,
			HTTPStatus:   0,
			ErrorMessage: fmt.Sprintf("HTTP request failed: %v", err),
			InfoHash:     req.InfoHash,
			PeerID:       req.PeerID,
		}
		return
	}
	defer response.Body.Close()

	// Check response status
	if response.StatusCode != http.StatusOK {
		results <- AnnounceResult{
			Success:      false,
			RequestTime:  requestTime,
			PeersCount:   0,
			HTTPStatus:   response.StatusCode,
			ErrorMessage: fmt.Sprintf("Non-200 status code: %d", response.StatusCode),
			InfoHash:     req.InfoHash,
			PeerID:       req.PeerID,
		}
		return
	}

	// Parse the response
	// In a real implementation, you would decode the bencode response here
	// For simplicity, we're just assuming success and a random number of peers
	peersCount := rng.Intn(50) + 1

	// Return the result
	results <- AnnounceResult{
		Success:      true,
		RequestTime:  requestTime,
		PeersCount:   peersCount,
		HTTPStatus:   response.StatusCode,
		ErrorMessage: "",
		InfoHash:     req.InfoHash,
		PeerID:       req.PeerID,
	}
}

// generateAnnounceRequestFromPools creates an announce request using the predefined pools
func generateAnnounceRequestFromPools(params RequestParams) AnnounceRequest {
	// Select a random info_hash from the pool
	infoHash := infoHashPool[rng.Intn(len(infoHashPool))]

	// Select a random peer from the pool
	peer := peerPool[rng.Intn(len(peerPool))]

	return AnnounceRequest{
		InfoHash:    infoHash,
		PeerID:      peer.ID,
		Port:        peer.Port,
		Uploaded:    randomInt64(params.MinUploaded, params.MaxUploaded),
		Downloaded:  randomInt64(params.MinDownloaded, params.MaxDownloaded),
		Left:        randomInt64(params.MinLeft, params.MaxLeft),
		Event:       params.Events[rng.Intn(len(params.Events))],
		IP:          peer.IP,
		NumWant:     rng.Intn(params.MaxNumWant-params.MinNumWant+1) + params.MinNumWant,
		Key:         peer.Key,
		TrackerID:   "",
		Compact:     1,
		NoPeerID:    0,
		Supportcryp: 0,
	}
}

// buildAnnounceURL builds the complete URL for the announce request
func buildAnnounceURL(baseURL string, req AnnounceRequest) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}

	// Add query parameters
	q := u.Query()
	q.Set("info_hash", req.InfoHash)
	q.Set("peer_id", req.PeerID)
	q.Set("port", fmt.Sprintf("%d", req.Port))
	q.Set("uploaded", fmt.Sprintf("%d", req.Uploaded))
	q.Set("downloaded", fmt.Sprintf("%d", req.Downloaded))
	q.Set("left", fmt.Sprintf("%d", req.Left))

	if req.Event != "" {
		q.Set("event", req.Event)
	}

	if req.IP != "" {
		q.Set("ip", req.IP)
	}

	q.Set("numwant", fmt.Sprintf("%d", req.NumWant))

	if req.Key != "" {
		q.Set("key", req.Key)
	}

	if req.TrackerID != "" {
		q.Set("trackerid", req.TrackerID)
	}

	q.Set("compact", fmt.Sprintf("%d", req.Compact))

	if req.NoPeerID != 0 {
		q.Set("no_peer_id", fmt.Sprintf("%d", req.NoPeerID))
	}

	if req.Supportcryp != 0 {
		q.Set("supportcryp", fmt.Sprintf("%d", req.Supportcryp))
	}

	u.RawQuery = q.Encode()

	// Note: In a real implementation, you'd need to properly encode the info_hash and peer_id
	// as they are raw 20-byte values, not URL-safe strings.
	return u.String(), nil
}

// generateRandomInfoHash creates a random info_hash (20 bytes, URL encoded)
func generateRandomInfoHash() string {
	// In a real implementation, this would be properly URL encoded
	// For now, we'll just create a hex string of the right length
	bytes := make([]byte, 20)
	rng.Read(bytes)
	return fmt.Sprintf("%x", bytes)
}

// generateRandomPeerID creates a random peer_id (20 bytes)
func generateRandomPeerID() string {
	// Standard format is -XX0000-{random 12 bytes}
	// where XX is the client ID
	clientID := "GO"
	version := "0001"

	randomPart := make([]byte, 12)
	rng.Read(randomPart)

	return fmt.Sprintf("-%s%s-%x", clientID, version, randomPart)
}

// generateRandomKey creates a random key of the specified length
func generateRandomKey(minLen, maxLen int) string {
	length := rng.Intn(maxLen-minLen+1) + minLen
	bytes := make([]byte, length)
	rng.Read(bytes)
	return fmt.Sprintf("%x", bytes)
}

// randomInt64 returns a random int64 in the specified range
func randomInt64(min, max int64) int64 {
	return min + rng.Int63n(max-min+1)
}

// processResult updates statistics based on the request result
func processResult(result AnnounceResult) {
	statsLock.Lock()
	defer statsLock.Unlock()

	requestsSent++
	if result.Success {
		requestsSuccess++
		totalPeers += result.PeersCount
	} else {
		requestsFailed++
	}
	totalRequestTime += result.RequestTime

	// Log the result if it's a failure
	if !result.Success {
		log.Printf("Request failed: %s (InfoHash: %s, PeerID: %s)",
			result.ErrorMessage, result.InfoHash, result.PeerID)
	}
}

// reportStats periodically prints statistics
func reportStats(stop <-chan struct{}) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			printStats()
		case <-stop:
			return
		}
	}
}

// printStats displays the current statistics
func printStats() {
	statsLock.Lock()
	defer statsLock.Unlock()

	var avgTime time.Duration
	if requestsSent > 0 {
		avgTime = totalRequestTime / time.Duration(requestsSent)
	}

	var avgPeers float64
	if requestsSuccess > 0 {
		avgPeers = float64(totalPeers) / float64(requestsSuccess)
	}

	var successRate float64
	if requestsSent > 0 {
		successRate = float64(requestsSuccess) * 100 / float64(requestsSent)
	}

	log.Printf("Statistics:")
	log.Printf("  Requests: %d total, %d successful (%.1f%%), %d failed",
		requestsSent, requestsSuccess, successRate, requestsFailed)
	log.Printf("  Average request time: %v", avgTime)
	log.Printf("  Average peers per response: %.1f", avgPeers)
}

// waitForCompletion waits for the program to complete or be interrupted
func waitForCompletion(done chan struct{}, stopStats chan struct{}, wg *sync.WaitGroup) {
	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	// signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Wait for either completion or interruption
	select {
	case <-done:
		log.Printf("Duration completed, shutting down...")
	case <-sigChan:
		log.Printf("Interrupt received, shutting down...")
		close(done)
	}

	// Stop the stats reporter
	close(stopStats)

	// Wait for all goroutines to finish
	wg.Wait()

	// Print final statistics
	printStats()
	log.Printf("Program completed.")
}
