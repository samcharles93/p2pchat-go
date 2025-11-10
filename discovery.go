package main

import (
	"fmt"
	"log"
	"net"
	"strings"
	"time"
)

func (n *Node) handleDiscovery() {
	defer n.wg.Done()

	buffer := make([]byte, 1024)
	for {
		select {
		case <-n.Shutdown:
			return
		default:
			n.discoveryConn.SetReadDeadline(time.Now().Add(1 * time.Second))
			length, addr, err := n.discoveryConn.ReadFromUDP(buffer)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				select {
				case <-n.Shutdown:
					return
				default:
					log.Printf("Discovery read error: %v", err)
					continue
				}
			}

			message := string(buffer[:length])
			parts := strings.Split(message, string(delimiter))

			if len(parts) < 2 {
				continue
			}

			command := parts[0]
			peerID := parts[1]

			switch command {
			case "DISCOVER":
				// Respond to discovery
				if peerID != n.ID {
					n.knownMutex.Lock()
					n.KnownPeers[peerID] = true
					n.knownMutex.Unlock()

					select {
					case n.DiscoveredPeer <- peerID:
					default:
					}

					// Send response
					response := fmt.Sprintf("DISCOVER_RESPONSE%c%s", delimiter, n.ID)
					n.discoveryConn.WriteToUDP([]byte(response), addr)
				}

			case "DISCOVER_RESPONSE":
				if peerID != n.ID {
					n.knownMutex.Lock()
					n.KnownPeers[peerID] = true
					n.knownMutex.Unlock()

					select {
					case n.DiscoveredPeer <- peerID:
					default:
					}
				}
			}
		}
	}
}

func (n *Node) announcePresence() {
	defer n.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	mcastAddr, _ := net.ResolveUDPAddr("udp", multicastAddr)

	for {
		select {
		case <-ticker.C:
			message := fmt.Sprintf("DISCOVER%c%s", delimiter, n.ID)
			n.discoveryConn.WriteToUDP([]byte(message), mcastAddr)

		case <-n.Shutdown:
			return
		}
	}
}
