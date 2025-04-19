
# torrent-tracker-tester
Tool to make fake concurrent requests to BitTorrent Trackers

While developing a tracker is important to test it at scale with lots of concurrent clients reaching for different torrents. 
This tool helps with that! It supports both HTTP and UDP trackers. IPv4 only thought (for now).
Useful statistic are printed out every few seconds while requests fly and try to take down the server.

## Setup
You can compile it with 

> go build

 and executing **torrent-tracker-tester** or run the project directly using 

> go run main.go

## Usage
The current options are (displayed when using parameter *-help*):

      -clientpool int
            Size of the peer pool (default 50)
      -concurrent int
            Number of concurrent requests (default 1)
      -duration string
            Duration to run (e.g., 1m, 1h, 30s)
      -hashpool int
            Size of the info_hash pool (default 100)
      -http-tracker string
            HTTP Tracker URL
      -seeders float
            Probability of generating seeder requests (0.0-1.0) (default 0.3)
      -udp-tracker string
            UDP Tracker URL
      -verbose
            Verbose output (default true)

Example command-line to get going:

> go run main.go -concurrent=100 -http-tracker="http://localhost:6969/announce" -duration=0 -verbose=false -hashpool=100000 -clientpool=10000

Example statistics displayed:

    Statistics:
      Requests: 86746 total, 85839 successful (99.0%), 907 failed
      Types: 25947 seeders (29.9%), 60799 leechers (70.1%)
      Success rates: seeders 98.9%, leechers 99.0%
      Average request time: 63.106593ms
      Average peers per response: 21.5

