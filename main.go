package main

import (
	// "encoding/binary"
    "encoding/json"
	"log"
	"net"
	"os"
	"time"
)

const (
	queryPacket = "\xff\xff\xff\xffTSource Engine Query\x00"
	cacheFile   = "cache.dat"
	updateFreq  = time.Minute * 1 // update cache every 1 minutes
)

func main() {
	// List of IP:ports to create cache for
	connections := []string{
		"127.0.0.1:28017",
	}

	for {
		// Create cache map
        cache := make(map[string][]byte)

        // Loop over connections and send query to each one
        for _, connStr := range connections {
            conn, err := net.DialTimeout("udp", connStr, time.Second*5)
            if err != nil {
                log.Printf("Failed to connect to %s: %v", connStr, err)
                cache[connStr] = nil // Write nil to cache for connection that did not respond
                continue
            }
            defer conn.Close()

            // Send query packet
            if _, err := conn.Write([]byte(queryPacket)); err != nil {
                log.Printf("Failed to send query to %s: %v", connStr, err)
                cache[connStr] = nil // Write nil to cache for connection that did not respond
                continue
            }

            // Read response
            response := make([]byte, 2048)
            recieved := true
            if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
                log.Printf("Failed to set read deadline for %s: %v", connStr, err)
            }
            if _, err := conn.Read(response); err != nil {
                if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
                    log.Printf("Timed out while waiting for response from %s", connStr)
                } else {
                    log.Printf("Failed to read response from %s: %v", connStr, err)
                }
                recieved = false
                cache[connStr] = nil // Write nil to cache for connection that did not respond
                continue
            }


            log.Printf("%v\n", response)
            log.Printf("%v\n", string(response))

            // Cache response
            if (recieved) {
                log.Printf("Cached response to map.")
                cache[connStr] = response
            }

        }

        // Save cache to file
        cacheFile, err := os.Create("cache.dat")
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
