package main

import (
	"io/ioutil"
    "encoding/json"
	"log"
	"net"
	"os"
	"time"
)

const (
	queryPacket = "\xff\xff\xff\xffTSource Engine Query\x00"
	cacheFile   = "cache.json"
	updateFreq  = time.Minute * 1 // update cache every 1 minutes
)

func main() {  
	for {
        // Read server configuration from JSON file
        serversFile, err := ioutil.ReadFile("servers.json")
        if err != nil {
            log.Fatalf("Failed to read server configuration: %v", err)
        }

        var servers map[string]bool
        if err := json.Unmarshal(serversFile, &servers); err != nil {
            log.Fatalf("Failed to parse server configuration: %v", err)
        }

        connections := make([]string, 0, len(servers))
        for connStr := range servers {
            connections = append(connections, connStr)
        }

		// Create cache map and channels
        cache := make(map[string][]byte)
        responseCh := make(chan []byte)
        errorCh := make(chan error)

        // Loop over connections and start a goroutine for each one
        for _, connStr := range connections {
            go func(connStr string) {
                conn, err := net.DialTimeout("udp", connStr, time.Second*5)
                if err != nil {
                    log.Printf("Failed to connect to %s: %v", connStr, err)
                    errorCh <- err
                    return
                }
                defer conn.Close()

                // Send query packet
                if _, err := conn.Write([]byte(queryPacket)); err != nil {
                    log.Printf("Failed to send query to %s: %v", connStr, err)
                    errorCh <- err
                    return
                }

                // Read response
                response := make([]byte, 2048)
                if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil { // Set timeout for reading a response to 5 seconds
                    log.Printf("Failed to set read deadline for %s: %v", connStr, err)
                }
                if _, err := conn.Read(response); err != nil {
                    if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
                        log.Printf("Timed out while waiting for response from %s", connStr)
                    } else {
                        log.Printf("Failed to read response from %s: %v", connStr, err)
                    }
                    errorCh <- err
                    return
                }

                responseCh <- response
            }(connStr)
        }

        // Wait for responses from all connections and cache them
        for i := 0; i < len(connections); i++ {
            select {
            case response := <-responseCh:
                log.Printf("Received response: %v\n", string(response))
                cache[connections[i]] = response
            case err := <-errorCh:
                log.Printf("Encountered error: %v\n", err)
                cache[connections[i]] = nil // Write nil to cache for connection that did not respond
            }
        }

        // Remove servers that are no longer in the configuration
        for connStr := range cache {
            if !servers[connStr] {
                delete(cache, connStr)
            }
        }

        // Save cache to file
        cacheFile, err := os.Create("cache.json")
        if err != nil {
            log.Fatalf("Failed to create cache file: %v", err)
        }
        defer cacheFile.Close()

        cacheJSON, err := json.Marshal(cache)
        if err != nil {
            log.Fatalf("Failed to encode cache as JSON: %v", err)
        }

        if _, err := cacheFile.Write(cacheJSON); err != nil {
            log.Fatalf("Failed to save cache: %v", err)
        }

		log.Println("Cache saved successfully.")

		// Wait for the update frequency before next update
		time.Sleep(updateFreq)
	}
}
