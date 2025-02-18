FROM golang:1.22 AS builder
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 go mod tidy && \
    CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-extldflags "-static"' -o road_traffic_exporter ./cmd/road_traffic_exporter

FROM alpine:latest
WORKDIR /app
RUN apk add --no-cache curl
COPY --from=builder /app/road_traffic_exporter .
COPY web/ui.html web/
CMD ["/app/road_traffic_exporter"]
EXPOSE 8080
