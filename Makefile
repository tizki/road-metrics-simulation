.PHONY: build run rerun

IMAGE_NAME=road_traffic_exporter

build:
	go build -o bin/road_traffic_exporter cmd/road_traffic_exporter/main.go

docker-build:
	docker build -t $(IMAGE_NAME) .

docker-run:
	docker run --rm -p 8080:8080 $(IMAGE_NAME)

clean:
	rm -f road_traffic_exporter

run:
	docker-compose up --build

rerun:
	docker-compose down
	docker volume prune -f
	docker-compose up --build

.DEFAULT_GOAL := run
