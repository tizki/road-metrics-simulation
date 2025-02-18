package main

import (
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/prometheus/prompb"
)

type Road struct {
	name        string
	currentCars float64
	mutex       sync.RWMutex
	entryRate   int64
	exitRate    int64
	capacity    int64
	minDelay    int64
}

var (
	carCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "road_traffic_total",
			Help: "Total number of cars passing on the road.",
		},
		[]string{"road", "color", "maker"},
	)
	carsOnRoad = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cars_on_road",
			Help: "Number of cars currently on the road.",
		},
		[]string{"road"},
	)
	trafficPattern = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "traffic_pattern",
			Help: "Current traffic pattern (1=night, 2=normal, 3=rush_hour)",
		},
		[]string{"road"},
	)

	// Default configuration for roads
	defaultEntryRate int64 = 1
	defaultExitRate  int64 = 1
	defaultCapacity  int64 = 100
	defaultMinDelay  int64 = 500

	// Map to store all roads
	roads      = make(map[string]*Road)
	roadsMutex sync.RWMutex
)

func NewRoad(name string) *Road {
	return &Road{
		name:      name,
		entryRate: defaultEntryRate,
		exitRate:  defaultExitRate,
		capacity:  defaultCapacity,
		minDelay:  defaultMinDelay,
	}
}

func init() {
	prometheus.MustRegister(carCounter, carsOnRoad, trafficPattern)
	rand.Seed(time.Now().UnixNano())
}

func (r *Road) simulateCarEntry() {
	colors := []string{"red", "blue", "green", "black", "white"}
	makers := []string{"Toyota", "Ford", "BMW", "Tesla", "Honda"}

	for {
		currentRate := atomic.LoadInt64(&r.entryRate)
		delay := time.Duration(rand.Int63n(1000)/currentRate+atomic.LoadInt64(&r.minDelay)) * time.Millisecond

		r.mutex.RLock()
		canAdd := r.currentCars < float64(r.capacity)
		r.mutex.RUnlock()

		if canAdd {
			color := colors[rand.Intn(len(colors))]
			maker := makers[rand.Intn(len(makers))]

			r.mutex.Lock()
			r.currentCars++
			carsOnRoad.WithLabelValues(r.name).Set(r.currentCars)
			r.mutex.Unlock()

			carCounter.WithLabelValues(r.name, color, maker).Inc()
		}

		time.Sleep(delay)
	}
}

func (r *Road) simulateCarExit() {
	for {
		currentRate := atomic.LoadInt64(&r.exitRate)
		delay := time.Duration(rand.Int63n(1000)/currentRate+atomic.LoadInt64(&r.minDelay)) * time.Millisecond

		r.mutex.RLock()
		canRemove := r.currentCars > 0
		r.mutex.RUnlock()

		if canRemove {
			r.mutex.Lock()
			r.currentCars--
			carsOnRoad.WithLabelValues(r.name).Set(r.currentCars)
			r.mutex.Unlock()
		}

		time.Sleep(delay)
	}
}

func setRate(w http.ResponseWriter, r *http.Request) {
	roadName := r.URL.Query().Get("road")
	rate := r.URL.Query().Get("rate")

	roadsMutex.RLock()
	road, exists := roads[roadName]
	roadsMutex.RUnlock()

	if !exists {
		http.Error(w, fmt.Sprintf("Road %s not found", roadName), http.StatusNotFound)
		return
	}

	// Get current cars before changing rates
	road.mutex.RLock()
	currentCars := road.currentCars
	road.mutex.RUnlock()

	// Define target cars for each pattern
	var targetCars float64
	switch rate {
	case "rush_hour":
		atomic.StoreInt64(&road.entryRate, 8)
		atomic.StoreInt64(&road.exitRate, 2)
		atomic.StoreInt64(&road.capacity, 150)
		atomic.StoreInt64(&road.minDelay, 100)
		trafficPattern.WithLabelValues(road.name).Set(3)
		targetCars = float64(road.capacity) * 0.8 // 80% of capacity for rush hour
	case "night":
		atomic.StoreInt64(&road.entryRate, 1)    // Keep at minimum
		atomic.StoreInt64(&road.exitRate, 12)    // Increased exit rate even more
		atomic.StoreInt64(&road.capacity, 50)    // Keep reduced capacity
		atomic.StoreInt64(&road.minDelay, 10000) // Much longer delay (10 seconds)
		trafficPattern.WithLabelValues(road.name).Set(1)
		targetCars = float64(road.capacity) * 0.05 // Only 5% of capacity for night
	default: // normal daytime
		atomic.StoreInt64(&road.entryRate, 3)
		atomic.StoreInt64(&road.exitRate, 4)    // Slightly higher exit rate
		atomic.StoreInt64(&road.capacity, 100)  // Lower capacity
		atomic.StoreInt64(&road.minDelay, 1000) // Increased delay
		trafficPattern.WithLabelValues(road.name).Set(2)
		targetCars = float64(road.capacity) * 0.4 // 40% of capacity for normal
	}

	// If we need to reduce traffic, rapidly remove cars
	if currentCars > targetCars {
		road.mutex.Lock()
		adjustment := currentCars - targetCars
		road.currentCars -= adjustment
		carsOnRoad.WithLabelValues(road.name).Set(road.currentCars)
		road.mutex.Unlock()
	}

	fmt.Fprintf(w, "Traffic pattern for road %s set to %s", roadName, rate)
}

func backfillData(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Backfill request received")
	now := time.Now().UTC()

	// Based on Prometheus constraints:
	// 1. Must be newer than last block's maxTime
	// 2. Must be within tsdb.min-block-duration/2 (1h) of head block
	// So we'll backfill from (now - 1h) to (now - 1min)
	endTime := now.Add(-1 * time.Minute).Truncate(time.Minute)
	startTime := endTime.Add(-55 * time.Minute) // Leave some margin from the 1h limit

	fmt.Printf("Generating data from %v to %v (now: %v)\n", startTime, endTime, now)
	fmt.Printf("Gap between backfill end and now: %v\n", now.Sub(endTime))
	w.Header().Set("Content-Type", "application/x-protobuf")

	// Pre-calculate all timestamps
	var timestamps []time.Time
	for t := startTime; t.Before(endTime); t = t.Add(5 * time.Minute) {
		timestamps = append(timestamps, t)
	}

	fmt.Printf("Pre-calculated %d timestamps\n", len(timestamps))
	fmt.Printf("Start timestamp: %d, End timestamp: %d, Current timestamp: %d\n",
		startTime.UnixMilli(), endTime.UnixMilli(), now.UnixMilli())

	// Pre-calculate all data points
	var timeseries []prompb.TimeSeries
	totals := make(map[string]float64)
	colors := []string{"red", "blue", "green", "black", "white"}
	makers := []string{"Toyota", "Ford", "BMW", "Tesla", "Honda"}

	// Initialize totals map
	for _, color := range colors {
		for _, maker := range makers {
			totals[color+"/"+maker] = 0
		}
	}

	// Define travel times in minutes for different conditions
	const (
		emptyRoadTime     = 10.0 // 10 minutes when empty
		normalTrafficTime = 25.0 // 25 minutes in normal traffic
		rushHourTime      = 40.0 // 40 minutes in rush hour
	)

	// Generate all data points first
	type dataPoint struct {
		timestamp int64
		roadData  map[string]struct {
			cars   map[string]float64 // Current number of cars by color/maker
			totals map[string]float64 // Cumulative total traffic by color/maker
		}
	}
	dataPoints := make([]dataPoint, 0, len(timestamps))

	// Track current cars on road for each road
	currentCars := make(map[string]map[string]float64)
	// Track cars that entered in the last 40 minutes (max travel time) for each road
	recentEntries := make(map[string][]struct {
		time  time.Time
		color string
		maker string
		count float64
	})

	// Initialize tracking maps for each road
	roadsMutex.RLock()
	for roadName := range roads {
		currentCars[roadName] = make(map[string]float64)
		recentEntries[roadName] = make([]struct {
			time  time.Time
			color string
			maker string
			count float64
		}, 0, 100)

		for _, color := range colors {
			for _, maker := range makers {
				currentCars[roadName][color+"/"+maker] = 0
			}
		}
	}
	roadsMutex.RUnlock()

	for _, t := range timestamps {
		hourOfDay := t.Hour()
		timestamp := t.UnixMilli()

		dp := dataPoint{
			timestamp: timestamp,
			roadData: make(map[string]struct {
				cars   map[string]float64
				totals map[string]float64
			}),
		}

		roadsMutex.RLock()
		for roadName := range roads {
			// Initialize data for this road
			dp.roadData[roadName] = struct {
				cars   map[string]float64
				totals map[string]float64
			}{
				cars:   make(map[string]float64),
				totals: make(map[string]float64),
			}

			// Calculate base rate and travel time for this period
			var (
				baseRate   float64
				travelTime float64
			)
			switch {
			case hourOfDay >= 7 && hourOfDay <= 9: // Morning rush
				baseRate = 4.0
				travelTime = rushHourTime
			case hourOfDay >= 16 && hourOfDay <= 18: // Evening rush
				baseRate = 4.0
				travelTime = rushHourTime
			case hourOfDay >= 23 || hourOfDay <= 4: // Night
				baseRate = 0.5
				travelTime = emptyRoadTime
			default: // Normal daytime
				baseRate = 2.0
				travelTime = normalTrafficTime
			}

			// Process data for this road
			cutoffTime := t.Add(-time.Duration(travelTime) * time.Minute)
			newRecentEntries := make([]struct {
				time  time.Time
				color string
				maker string
				count float64
			}, 0)

			// Reset current cars count for this road
			for k := range currentCars[roadName] {
				currentCars[roadName][k] = 0
			}

			// Keep only cars still on the road
			for _, entry := range recentEntries[roadName] {
				if entry.time.After(cutoffTime) {
					newRecentEntries = append(newRecentEntries, entry)
					key := entry.color + "/" + entry.maker
					currentCars[roadName][key] += entry.count
				}
			}
			recentEntries[roadName] = newRecentEntries

			// Generate new entries for this time period
			for _, color := range colors {
				for _, maker := range makers {
					key := color + "/" + maker
					// Calculate new cars entering
					entering := baseRate * (0.5 + rand.Float64())

					// Add to current cars and totals
					currentCars[roadName][key] += entering
					dp.roadData[roadName].cars[key] = currentCars[roadName][key]
					dp.roadData[roadName].totals[key] = totals[key] + entering
					totals[key] = dp.roadData[roadName].totals[key]

					// Add new cars to recent entries
					recentEntries[roadName] = append(recentEntries[roadName], struct {
						time  time.Time
						color string
						maker string
						count float64
					}{t, color, maker, entering})
				}
			}
		}
		roadsMutex.RUnlock()

		dataPoints = append(dataPoints, dp)
	}

	// Generate time series for each road
	for _, dp := range dataPoints {
		for roadName, roadData := range dp.roadData {
			for _, color := range colors {
				for _, maker := range makers {
					key := color + "/" + maker

					// Add cars_on_road gauge
					timeseries = append(timeseries, prompb.TimeSeries{
						Labels: []prompb.Label{
							{Name: "__name__", Value: "cars_on_road"},
							{Name: "road", Value: roadName},
							{Name: "color", Value: color},
							{Name: "maker", Value: maker},
						},
						Samples: []prompb.Sample{
							{Value: roadData.cars[key], Timestamp: dp.timestamp},
						},
					})

					// Add road_traffic_total counter
					timeseries = append(timeseries, prompb.TimeSeries{
						Labels: []prompb.Label{
							{Name: "__name__", Value: "road_traffic_total"},
							{Name: "road", Value: roadName},
							{Name: "color", Value: color},
							{Name: "maker", Value: maker},
						},
						Samples: []prompb.Sample{
							{Value: roadData.totals[key], Timestamp: dp.timestamp},
						},
					})
				}
			}
		}
	}

	fmt.Printf("Generated %d time series with %d samples each\n", len(timeseries), len(dataPoints))
	writeRequest := &prompb.WriteRequest{
		Timeseries: timeseries,
	}
	data, err := proto.Marshal(writeRequest)
	if err != nil {
		fmt.Printf("Error marshaling data: %v\n", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	compressed := snappy.Encode(nil, data)
	fmt.Printf("Sending %d bytes of compressed data\n", len(compressed))
	w.Write(compressed)
	fmt.Printf("Backfill data sent successfully. Time range: %v to %v\n",
		time.UnixMilli(startTime.UnixMilli()),
		time.UnixMilli(endTime.UnixMilli()))
}

func main() {
	// Initialize roads
	roadNames := []string{"road-1", "road-6", "Ayalon", "road-90", "road-431"}
	for _, name := range roadNames {
		road := NewRoad(name)
		roadsMutex.Lock()
		roads[name] = road
		roadsMutex.Unlock()

		// Start simulators for each road
		for i := 0; i < 5; i++ {
			go road.simulateCarEntry()
		}
		for i := 0; i < 3; i++ {
			go road.simulateCarExit()
		}
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "web/ui.html")
	})
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/set_rate", setRate)
	http.HandleFunc("/backfill", backfillData)
	fmt.Println("Prometheus metrics server running on :8080/metrics")
	http.ListenAndServe(":8080", nil)
}
