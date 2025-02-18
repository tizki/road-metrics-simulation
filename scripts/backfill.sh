#!/bin/sh
set -e  # Exit on any error

echo "Waiting for services to start..."
sleep 30  # Increased initial wait

MAX_RETRIES=3
RETRY_COUNT=0

while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
    echo "Fetching backfill data (attempt $((RETRY_COUNT + 1))/$MAX_RETRIES)..."
    
    # Fetch data with full response for debugging
    HTTP_RESPONSE=$(curl -v -H "Content-Type: application/x-protobuf" \
        http://road_traffic_exporter:8080/backfill \
        --output /tmp/backfill.dat \
        --write-out "HTTPSTATUS:%{http_code}" 2>/tmp/curl.log)
    
    HTTP_STATUS=$(echo "$HTTP_RESPONSE" | tr -d '\n' | sed -e 's/.*HTTPSTATUS://')
    
    if [ "$HTTP_STATUS" = "200" ]; then
        echo "Backfill data fetched successfully"
        echo "Sending data to Prometheus..."
        
        # Send to Prometheus with detailed output
        PROM_RESPONSE=$(curl -v -X POST \
            -H "Content-Type: application/x-protobuf" \
            -H "Content-Encoding: snappy" \
            --data-binary @/tmp/backfill.dat \
            http://prometheus:9090/api/v1/write 2>&1)
        
        PROM_STATUS=$?
        
        if [ $PROM_STATUS -eq 0 ]; then
            echo "Data sent to Prometheus successfully"
            echo "Backfill complete"
            echo "Backfill data size: $(wc -c < /tmp/backfill.dat) bytes"
            
            # Verify data exists with more attempts and longer waits
            echo "Verifying data in Prometheus..."
            VERIFY_RETRIES=10
            VERIFY_SUCCESS=0
            
            # Initial wait after sending data
            echo "Waiting 30 seconds for Prometheus to process data..."
            sleep 30
            
            while [ $VERIFY_RETRIES -gt 0 ]; do
                echo "Verification attempt $((11 - VERIFY_RETRIES))/10..."
                
                # Query specifically for data from 1 hour ago
                NOW=$(date +%s)
                HOUR_AGO=$((NOW - 3600))
                
                VERIFY_RESPONSE=$(curl -s "http://prometheus:9090/api/v1/query?query=count(cars_on_road[1h]) > 0&time=$NOW")
                echo "Verification response: $VERIFY_RESPONSE"
                
                if echo "$VERIFY_RESPONSE" | grep -q '"value":\[.*,"1"\]'; then
                    echo "Historic data found in Prometheus!"
                    VERIFY_SUCCESS=1
                    break
                fi
                
                VERIFY_RETRIES=$((VERIFY_RETRIES - 1))
                if [ $VERIFY_RETRIES -gt 0 ]; then
                    echo "Waiting 15 seconds before next attempt..."
                    sleep 15
                fi
            done
            
            if [ $VERIFY_SUCCESS -eq 1 ]; then
                echo "Backfill verification successful"
                exit 0
            else
                echo "Failed to verify data after multiple attempts"
                # Show what data is available
                echo "Checking available data ranges..."
                curl -s "http://prometheus:9090/api/v1/query?query=cars_on_road[2h]" | jq .
            fi
        else
            echo "Failed to send to Prometheus"
            echo "Curl exit code: $PROM_STATUS"
            echo "Prometheus response:"
            echo "$PROM_RESPONSE"
        fi
    else
        echo "Failed to fetch backfill data"
        echo "HTTP Status: $HTTP_STATUS"
        echo "Curl logs:"
        cat /tmp/curl.log
    fi
    
    RETRY_COUNT=$((RETRY_COUNT + 1))
    [ $RETRY_COUNT -lt $MAX_RETRIES ] && sleep 5
done

echo "Failed after $MAX_RETRIES attempts"
exit 1 