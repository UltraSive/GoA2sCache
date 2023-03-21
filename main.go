package main

import (
	"io/ioutil"
    "encoding/json"
	"log"
	"net"
	"time"
)

const (
	queryPacket = "\xff\xff\xff\xffTSource Engine Query\x00"
	cacheFile   = "cache.json"
	updateFreq  = time.Second * 15 // update cache every 15 seconds
)

// Map data type of cached responses
var cache = make(map[string][]byte)

// Create a map to hold the goroutines for each address in the cache
var addressListeners = make(map[string]chan bool)

func listenAndServe(addr string, data []byte) {
    log.Printf("Serving from: %s\n", addr)

    udpAddr, err := net.ResolveUDPAddr("udp", addr)
    if err != nil {
        log.Fatalf("Failed to resolve UDP address: %s", err)
    }

    conn, err := net.ListenUDP("udp", udpAddr)
    if err != nil {
        log.Fatalf("Failed to listen on UDP: %s", err)
    }
    defer conn.Close()

    for {
        buffer := make([]byte, 1024)
        n, clientAddr, err := conn.ReadFromUDP(buffer)
        if err != nil {
            log.Printf("Failed to read UDP message: %s", err)
            continue
        }

        if string(buffer[:n]) == queryPacket {
            _, err := conn.WriteToUDP(data, clientAddr)
            if err != nil {
                log.Printf("Failed to respond to UDP message: %s", err)
            }
            log.Printf("Responding to client: %s\n", clientAddr)
        }
    }
}

func main() {   
	for {
        // Show what the current cache map looks like
        log.Printf("Current cache status: %v\n", cache)

        // Read server configuration from JSON file
        serversFile, err := ioutil.ReadFile("servers.json")
        if err != nil {
            log.Fatalf("Failed to read server configuration: %v", err)
        }

        var servers map[string]bool
        if err := json.Unmarshal(serversFile, &servers); err != nil {
            log.Fatalf("Failed to parse server configuration: %v", err)
        }

        // Might be unncessesary to have this variable
        connections := make([]string, 0, len(servers))
        for connStr := range servers {
            connections = append(connections, connStr)
        }

		// Create channels for go routine
        responseCh := make(chan struct {
            connStr string
            data    []byte
        })
        errorCh := make(chan struct {
			connStr string
			err     error
		})

        // Stop any existing goroutines for addresses that are no longer in the cache
        for addr, done := range addressListeners {
            if _, ok := cache[addr]; !ok {
                done <- true
                delete(addressListeners, addr)
            }
        }

        // Start new goroutines for addresses that are in the cache but don't have a goroutine yet
        for addr, data := range cache {
            if _, ok := addressListeners[addr]; !ok {
                done := make(chan bool)
                addressListeners[addr] = done
                go func(addr string, data []byte, done chan bool) {
                    listenAndServe(addr, data)
                    close(done)
                }(addr, data, done)
            }
        }

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
                challengeSent := false
				for {
                    response := make([]byte, 300)
					conn.SetReadDeadline(time.Now().Add(5 * time.Second)) // Set timeout for reading a response to 5 seconds
					if _, err := conn.Read(response); err != nil {
                        log.Printf("%s -> %s", connStr, response)
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
						continue
					} else {
						if response[4] == 0x49 { 
							responseCh <- struct {
								connStr string
								data    []byte
							}{
								connStr: connStr,
								data:    response,
							}
							break
						} else if response[4] == 0x41 && !challengeSent { // Reply to the challenge for A2S_info
                            log.Printf("Challenge Detected")
                            next4Bytes := response[5:9]
                            challengeQueryPacket := queryPacket + string(next4Bytes)
                            conn.Write([]byte(challengeQueryPacket))
                            challengeSent = true
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
            case err := <-errorCh:
                log.Printf("Encountered error: %v\n", err)
                // cache[err.connStr] = nil // Write nil to cache for connection that did not respond
            }

            numResponsesReceived++

            if numResponsesReceived == len(connections) {
                break
            }
        }
	}
}
