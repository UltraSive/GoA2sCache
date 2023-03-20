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
	updateFreq  = time.Second * 15 // update cache every 15 seconds
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
        responseCh := make(chan struct {
            connStr string
            data    []byte
        })
        errorCh := make(chan struct {
			connStr string
			err     error
		})

        // Loop over connections and start a goroutine for each one
		for _, connStr := range connections {
			go func(connStr string) {
				conn, err := net.DialTimeout("udp", connStr, time.Second*5)
				if err != nil {
					log.Printf("Failed to connect to %s: %v", connStr, err)
					errorCh <- struct {
						connStr string
						err     error
					}{
						connStr: connStr,
						err:     err,
					}
					return
				}
				defer conn.Close()

				// Send query packet
				if _, err := conn.Write([]byte(queryPacket)); err != nil {
					log.Printf("Failed to send query to %s: %v", connStr, err)
					errorCh <- struct {
						connStr string
						err     error
					}{
						connStr: connStr,
						err:     err,
					}
					return
				}

				// Read response
				response := make([]byte, 1400)
				for {
					if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil { // Set timeout for reading a response to 5 seconds
						log.Printf("Failed to set read deadline for %s: %v", connStr, err)
					}
					if _, err := conn.Read(response); err != nil {
						if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
							log.Printf("Timed out while waiting for response from %s", connStr)

						} else {
							log.Printf("Failed to read response from %s: %v", connStr, err)
						}
						errorCh <- struct {
							connStr string
							err     error
						}{
							connStr: connStr,
							err:     err,
						}
						return
					} else {
						if response[4] == 0x49 {
							responseCh <- struct {
								connStr string
								data    []byte
							}{
								connStr: connStr,
								data:    response,
							}
							return
						}
					}
				}
			}(connStr)
		}

        // Wait for responses from all connections and cache them
        numResponsesReceived := 0
        for {
            select {
            case response := <-responseCh:
                log.Printf("Received response from %s: %v\n", response.connStr, string(response.data))
                cache[response.connStr] = response.data
                log.Printf("%s\n", cache[response.connStr])
            case err := <-errorCh:
                log.Printf("Encountered error: %v\n", err)
                cache[err.connStr] = nil // Write nil to cache for connection that did not respond
            }

            numResponsesReceived++

            if numResponsesReceived == len(connections) {
                break
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
