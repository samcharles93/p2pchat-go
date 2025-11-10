package main

import (
	"fmt"
	"log"
	"strings"
)

func (n *Node) handleIncomingMessage(msg Message) {
	// Check for gossip messages
	content := string(msg.Content)
	if strings.HasPrefix(content, "GOSSIP_PEERS:") {
		peerListStr := strings.TrimPrefix(content, "GOSSIP_PEERS:")
		if peerListStr != "" {
			peerList := strings.Split(peerListStr, ",")
			n.PeerListGossip <- peerList
		}
		return
	}

	// Regular message - send to UI
	if n.uiChannel != nil {
		n.uiChannel <- msg
	}
}

func (n *Node) broadcast(msg Message) {
	networkMsg := fmt.Sprintf("%s%c%s", msg.SenderID, delimiter, string(msg.Content))

	n.peersMutex.RLock()
	defer n.peersMutex.RUnlock()

	for _, peer := range n.Peers {
		select {
		case peer.Send <- []byte(networkMsg):
		default:
			log.Printf("Peer %s send channel full, dropping message", peer.ID)
		}
	}
}

func (n *Node) sendPeerListGossip() {
	n.peersMutex.RLock()
	n.knownMutex.RLock()
	defer n.peersMutex.RUnlock()
	defer n.knownMutex.RUnlock()

	// Build peer list
	peerList := make([]string, 0, len(n.KnownPeers))
	for peer := range n.KnownPeers {
		if peer != n.ID {
			peerList = append(peerList, peer)
		}
	}

	if len(peerList) == 0 {
		return
	}

	// Send to all connected peers
	gossipMsg := fmt.Sprintf("GOSSIP_PEERS:%s", strings.Join(peerList, ","))

	for _, peer := range n.Peers {
		select {
		case peer.Send <- []byte(fmt.Sprintf("%s%c%s", n.ID, delimiter, gossipMsg)):
		default:
			log.Printf("Peer %s send channel full, dropping gossip", peer.ID)
		}
	}
}
