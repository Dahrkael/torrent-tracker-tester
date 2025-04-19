package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dahrkael/torrent-tracker-tester/internal/bittorrent"
)

// Configuration parameters for the application
type Config struct {
	// Number of concurrent goroutines to maintain
	ConcurrentRequests int
	// HTTP Tracker URL to send announce requests to
	HTTPTrackerURL string
	// UDP Tracker URL to send announce requests to
	UDPTrackerURL string
	// Run duration (0 means run indefinitely)
	Duration time.Duration
	// Timeout for requests
	RequestTimeout time.Duration
	// Whether to print detailed logs
	Verbose bool
	// Size of the info_hash pool
	InfoHashPoolSize int
	// Size of the client pool
	ClientPoolSize int
	// Probability of generating a seeder request (0.0-1.0)
	SeederProbability float64
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
	// Possible event values for non-seeders
	Events []string
	// Range for random IP address
	IPRange []string
	// Range for random numwant values
	MinNumWant, MaxNumWant int
	// Range for random key length
	MinKeyLength, MaxKeyLength int
}

// AnnounceResult holds the result of an announce request
type AnnounceResult struct {
	Success      bool
	RequestTime  time.Duration
	PeersCount   int
	ErrorMessage string
	InfoHash     string
	PeerID       string
	IsSeeder     bool // Flag to indicate if this was a seeder request
}

// Global variables
var (
	// Default configuration
	defaultConfig = Config{
		ConcurrentRequests: 1,
		HTTPTrackerURL:     "",
		UDPTrackerURL:      "",
		Duration:           0,
		RequestTimeout:     10 * time.Second,
		Verbose:            true,
		InfoHashPoolSize:   100,
		ClientPoolSize:     50,
		SeederProbability:  0.3, // 30% seeders by default
		RequestParams: RequestParams{
			MinPort:       1,
			MaxPort:       65535,
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
	seedersSent      int
	seedersSuccess   int
	leechersSent     int
	leechersSuccess  int
	totalRequestTime time.Duration
	totalPeers       int
	statsLock        sync.Mutex

	// Random source
	rng = rand.New(rand.NewSource(time.Now().UnixNano()))

	// Pools
	infoHashPool []string
	clientPool   []bittorrent.ClientConfig
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

	if config.HTTPTrackerURL == "" && config.UDPTrackerURL == "" {
		fmt.Fprintf(os.Stderr, "No defined target trackers")
		os.Exit(1)
	}

	log.Printf("Starting BitTorrent tracker announcer")
	if config.HTTPTrackerURL != "" {
		log.Printf("HTTP Target tracker: %s", config.HTTPTrackerURL)
	}
	if config.UDPTrackerURL != "" {
		log.Printf("UDP Target tracker: %s", config.UDPTrackerURL)
	}
	log.Printf("Concurrent requests: %d", config.ConcurrentRequests)
	log.Printf("Info hash pool size: %d", config.InfoHashPoolSize)
	log.Printf("Peer pool size: %d", config.ClientPoolSize)
	log.Printf("Seeder probability: %.2f (%.1f%%)", config.SeederProbability, config.SeederProbability*100)
	if config.Duration > 0 {
		log.Printf("Running for: %s", config.Duration)
	} else {
		log.Printf("Running indefinitely (press Ctrl+C to stop)")
	}

	// Generate pools
	generateInfoHashPool(config.InfoHashPoolSize)
	generateClientPool(config.ClientPoolSize, config.RequestParams)

	log.Printf("Generated %d unique info_hashes", len(infoHashPool))
	log.Printf("Generated %d unique peers", len(clientPool))

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

// generateClientPool creates a pool of random peers
func generateClientPool(size int, params RequestParams) {
	clientPool = make([]bittorrent.ClientConfig, size)
	for i := 0; i < size; i++ {
		clientPool[i] = bittorrent.ClientConfig{
			PeerID: generateRandomPeerID(),
			Address: bittorrent.Peer{
				IP:   generateRandomIPv4(),
				Port: rng.Intn(params.MaxPort-params.MinPort+1) + params.MinPort,
			},
		}
	}
}

// parseFlags parses command line flags and returns the configuration
func parseFlags() Config {
	config := defaultConfig

	// Define command line flags
	flag.IntVar(&config.ConcurrentRequests, "concurrent", defaultConfig.ConcurrentRequests, "Number of concurrent requests")
	flag.StringVar(&config.HTTPTrackerURL, "http-tracker", defaultConfig.HTTPTrackerURL, "HTTP Tracker URL")
	flag.StringVar(&config.UDPTrackerURL, "udp-tracker", defaultConfig.UDPTrackerURL, "UDP Tracker URL")
	durationStr := flag.String("duration", "", "Duration to run (e.g., 1m, 1h, 30s)")
	flag.BoolVar(&config.Verbose, "verbose", defaultConfig.Verbose, "Verbose output")
	flag.IntVar(&config.InfoHashPoolSize, "hashpool", defaultConfig.InfoHashPoolSize, "Size of the info_hash pool")
	flag.IntVar(&config.ClientPoolSize, "clientpool", defaultConfig.ClientPoolSize, "Size of the peer pool")
	flag.Float64Var(&config.SeederProbability, "seeders", defaultConfig.SeederProbability, "Probability of generating seeder requests (0.0-1.0)")

	// Parse flags
	flag.Parse()

	// Validate seeder probability
	if config.SeederProbability < 0 || config.SeederProbability > 1 {
		fmt.Fprintf(os.Stderr, "Invalid seeder probability: %.2f (must be between 0.0 and 1.0)\n", config.SeederProbability)
		os.Exit(1)
	}

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

	var trackerUrls []string
	if config.HTTPTrackerURL != "" {
		trackerUrls = append(trackerUrls, config.HTTPTrackerURL)
	}
	if config.UDPTrackerURL != "" {
		trackerUrls = append(trackerUrls, config.UDPTrackerURL)
	}

	trackerUrl := trackerUrls[rng.Int()%len(trackerUrls)]

	// Determine if this request should be a seeder based on probability
	isSeeder := rng.Float64() < config.SeederProbability

	// Select a random info_hash from the pool
	infoHash := infoHashPool[rng.Intn(len(infoHashPool))]

	// Select a random client from the pool
	client := clientPool[rng.Intn(len(clientPool))]

	// Transfer statistics for the announce request
	var stats bittorrent.TransferStats
	if isSeeder {
		// Seeders have left=0, usually no event, and potentially high upload values
		stats = bittorrent.TransferStats{
			Uploaded:   randomInt64(config.RequestParams.MinUploaded, config.RequestParams.MaxUploaded) * 2,   // Typically higher uploads for seeders
			Downloaded: randomInt64(config.RequestParams.MaxDownloaded/2, config.RequestParams.MaxDownloaded), // Downloaded is usually high (completed)
			Left:       0,                                                                                     // Estimated total size remaining
		}
	} else {
		// Leechers have non-zero left and various events
		stats = bittorrent.TransferStats{
			Uploaded:   randomInt64(config.RequestParams.MinUploaded, config.RequestParams.MaxUploaded),     // No bytes uploaded yet
			Downloaded: randomInt64(config.RequestParams.MinDownloaded, config.RequestParams.MaxDownloaded), // No bytes downloaded yet
			Left:       randomInt64(config.RequestParams.MinLeft, config.RequestParams.MaxLeft),             // Estimated total size remaining
		}
		//event = params.Events[rng.Intn(len(params.Events))]
	}

	var err error

	// Create the appropiate type of tracker to send the request to
	tracker, err := createTracker(trackerUrl, client, config)
	if err != nil {
	}

	// Log the request if verbose
	if config.Verbose {
		if isSeeder {
			log.Printf("Sending seeder request to: %s", config.HTTPTrackerURL)
		} else {
			log.Printf("Sending leecher request to: %s", config.HTTPTrackerURL)
		}
	}

	errorMessage := ""
	// Send the announcement and measure the time it takes to complete
	startTime := time.Now()
	peers, err := tracker.Announce(infoHash, stats)
	if err != nil {
		errorMessage = fmt.Sprintf("%v", err)
	}
	requestTime := time.Since(startTime)
	peersCount := len(peers)

	// Process the response and return the result
	results <- AnnounceResult{
		Success:      err == nil,
		RequestTime:  requestTime,
		PeersCount:   peersCount,
		ErrorMessage: errorMessage,
		InfoHash:     infoHash,
		PeerID:       client.PeerID,
		IsSeeder:     isSeeder,
	}
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

	randomPart := make([]byte, 6) // 12
	rng.Read(randomPart)

	return fmt.Sprintf("-%s%s-%x", clientID, version, randomPart)
}

func generateRandomIPv4() string {
	// Generate four random octets (0-255)
	octet1 := rand.Intn(256)
	octet2 := rand.Intn(256)
	octet3 := rand.Intn(256)
	octet4 := rand.Intn(256)

	// Format the IPv4 address as a text string
	return fmt.Sprintf("%d.%d.%d.%d", octet1, octet2, octet3, octet4)
}

// randomInt64 returns a random int64 in the specified range
func randomInt64(min, max int64) int64 {
	return min + rng.Int63n(max-min+1)
}

// createTracker creates a Tracker based on the URL type, configuration, and tracker URL.
func createTracker(trackerURL string, client bittorrent.ClientConfig, config Config) (bittorrent.Tracker, error) {
	if strings.HasPrefix(strings.ToLower(trackerURL), "http://") || strings.HasPrefix(strings.ToLower(trackerURL), "https://") {
		return bittorrent.NewHTTPTracker(trackerURL, client, config.RequestTimeout), nil
	} else if strings.HasPrefix(strings.ToLower(trackerURL), "udp://") {
		return bittorrent.NewUDPTracker(trackerURL, client, config.RequestTimeout)
	}
	return nil, fmt.Errorf("unsupported protocol in URL: %s", trackerURL)
}

// processResult updates statistics based on the request result
func processResult(result AnnounceResult) {
	statsLock.Lock()
	defer statsLock.Unlock()

	requestsSent++

	// Update seeder/leecher counts
	if result.IsSeeder {
		seedersSent++
		if result.Success {
			seedersSuccess++
		}
	} else {
		leechersSent++
		if result.Success {
			leechersSuccess++
		}
	}

	if result.Success {
		requestsSuccess++
		totalPeers += result.PeersCount
	} else {
		requestsFailed++
	}
	totalRequestTime += result.RequestTime

	// Log the result if it's a failure
	if !result.Success {
		peerType := "leecher"
		if result.IsSeeder {
			peerType = "seeder"
		}

		log.Printf("Request failed (%s): %s (InfoHash: %s, PeerID: %s)",
			peerType, result.ErrorMessage, result.InfoHash, result.PeerID)
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

	var seederRate float64
	if requestsSent > 0 {
		seederRate = float64(seedersSent) * 100 / float64(requestsSent)
	}

	var seederSuccessRate float64
	if seedersSent > 0 {
		seederSuccessRate = float64(seedersSuccess) * 100 / float64(seedersSent)
	}

	var leecherSuccessRate float64
	if leechersSent > 0 {
		leecherSuccessRate = float64(leechersSuccess) * 100 / float64(leechersSent)
	}

	log.Printf("Statistics:")
	log.Printf("  Requests: %d total, %d successful (%.1f%%), %d failed",
		requestsSent, requestsSuccess, successRate, requestsFailed)
	log.Printf("  Types: %d seeders (%.1f%%), %d leechers (%.1f%%)",
		seedersSent, seederRate, leechersSent, 100-seederRate)
	log.Printf("  Success rates: seeders %.1f%%, leechers %.1f%%",
		seederSuccessRate, leecherSuccessRate)
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
