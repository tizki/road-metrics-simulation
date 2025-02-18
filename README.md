# Road Traffic Metrics Simulator

A learning project that simulates road traffic and exports Prometheus metrics. This project helps understand:
- Prometheus metrics (counters and gauges)
- Metric collection and visualization
- Time series data
- Backfilling historical data

## Overview

The simulator creates a virtual road where:
- Cars enter and exit the road
- Each car has a color and maker
- Traffic patterns change throughout the day (rush hour, normal, night)
- Cars take different amounts of time to traverse the road based on traffic conditions:
  - Empty road (night): 10 minutes
  - Normal traffic: 25 minutes
  - Rush hour: 40 minutes

## Metrics

The system exports two main metrics:
1. `cars_on_road` (gauge) - Current number of cars on the road by color and maker
2. `road_traffic_total` (counter) - Total number of cars that have entered the road by color and maker

## Running the Project

### Option 1: Direct Run
1. Start the simulator:
```bash
go run main.go
```
2. The simulator will start exporting metrics on port 8080

### Option 2: Docker Compose
1. Start all services (simulator, Prometheus, Grafana):
```bash
docker compose up -d
```

2. Stop all services:
```bash
docker compose down
```

## Access Points

- Simulator: http://localhost:8080/ui.html
- Metrics endpoint: http://localhost:8080/metrics
- Prometheus UI: http://localhost:9090
- Grafana dashboard: http://localhost:3000
  - Default login: admin/admin