package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// EnhancedNode wraps the Node with additional features
type EnhancedNode struct {
	*Node
	fileManager   *FileTransferManager
	voiceManager  *VoiceMessageManager
	featuresDir   string
	peerIDMap     map[string]string // Maps connection peer ID -> actual node ID (listen address)
	peerIDMapLock sync.RWMutex
}

// NewEnhancedNode creates a new enhanced node with all features
func NewEnhancedNode(listenAddr string, disableDiscovery bool) (*EnhancedNode, error) {
	// Create base node
	node, err := NewNode(listenAddr, disableDiscovery)
	if err != nil {
		return nil, err
	}

	// Create features directory
	featuresDir := "./data"
	if err := os.MkdirAll(featuresDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create features directory: %w", err)
	}

	// Create crypto manager if not exists
	if node.cryptoManager == nil {
		crypto, err := NewCryptoManager("./keys")
		if err != nil {
			return nil, fmt.Errorf("failed to create crypto manager: %w", err)
		}
		node.cryptoManager = crypto
	}

	// Create file manager
	fileDir := filepath.Join(featuresDir, "files")
	fileManager := NewFileTransferManager(node, node.cryptoManager, fileDir)

	// Create voice manager
	voiceDir := filepath.Join(featuresDir, "voice")
	voiceManager := NewVoiceMessageManager(node, node.cryptoManager, voiceDir)

	enhancedNode := &EnhancedNode{
		Node:         node,
		fileManager:  fileManager,
		voiceManager: voiceManager,
		featuresDir:  featuresDir,
		peerIDMap:    make(map[string]string),
	}

	// Note: processMessages is integrated into StartEnhanced event loop
	// No separate goroutine needed to avoid race condition

	return enhancedNode, nil
}

// processMessages handles incoming encrypted messages
func (en *EnhancedNode) processMessages() {
	for {
		select {
		case msg := <-en.IncomingMsg:
			en.handleIncomingMessage(msg)
		case <-en.Shutdown:
			return
		}
	}
}

// handleIncomingMessage processes incoming messages and routes them to appropriate handlers
func (en *EnhancedNode) handleIncomingMessage(msg Message) {
	// Update peer ID mapping: connection ID -> actual node ID
	// This is crucial because connections use ephemeral ports, but we need the listen address
	if msg.FromPeerID != "" && msg.SenderID != "" && msg.SenderID != en.ID {
		en.peerIDMapLock.Lock()
		en.peerIDMap[msg.FromPeerID] = msg.SenderID
		en.peerIDMapLock.Unlock()
	}

	// Check for unencrypted key exchange message
	content := string(msg.Content)
	if strings.HasPrefix(content, "KEY_EXCHANGE:") {
		// Extract the public key
		publicKeyPEM := strings.TrimPrefix(content, "KEY_EXCHANGE:")
		en.handleKeyExchange(msg.SenderID, []byte(publicKeyPEM))
		return
	}

	// Check if message is encrypted
	var encryptedMsg EncryptedMessage
	if err := json.Unmarshal(msg.Content, &encryptedMsg); err == nil {
		// This is an encrypted message, decrypt it
		plaintext, msgType, err := en.cryptoManager.DecryptMessage(&encryptedMsg)
		if err != nil {
			log.Printf("Failed to decrypt message from %s: %v", msg.SenderID, err)
			return
		}

		// Route based on message type
		switch msgType {
		case "text":
			// Regular text message
			textMsg := Message{
				SenderID:   msg.SenderID,
				Content:    plaintext,
				FromPeerID: msg.FromPeerID,
				IsGossip:   msg.IsGossip,
			}
			// Pass to original handler
			en.handleDecryptedMessage(textMsg)

		case "file":
			// File transfer message
			var fileMsg FileMessage
			if err := json.Unmarshal(plaintext, &fileMsg); err != nil {
				log.Printf("Failed to parse file message: %v", err)
				return
			}
			en.fileManager.HandleFileMessage(msg.SenderID, fileMsg)

		case "voice":
			// Voice message
			var voiceMsg VoiceMessage
			if err := json.Unmarshal(plaintext, &voiceMsg); err != nil {
				log.Printf("Failed to parse voice message: %v", err)
				return
			}
			en.voiceManager.HandleVoiceMessage(msg.SenderID, voiceMsg)

		case "key_exchange":
			// Encrypted key exchange message (for key rotation)
			en.handleKeyExchange(msg.SenderID, plaintext)

		default:
			log.Printf("Unknown message type: %s", msgType)
		}
	} else {
		// This is a plain text message (legacy or system message)
		en.handleDecryptedMessage(msg)
	}
}

// handleDecryptedMessage processes decrypted or plain text messages
func (en *EnhancedNode) handleDecryptedMessage(msg Message) {
	// Check if it's a command
	content := string(msg.Content)
	if strings.HasPrefix(content, "/") {
		en.handleEnhancedCLICommand(content, msg.SenderID)
		return
	}

	// Regular message - send to UI only (broadcasting is handled by sender)
	if en.uiChannel != nil {
		en.uiChannel <- msg
	}
}

// handleEnhancedCLICommand processes enhanced CLI commands
func (en *EnhancedNode) handleEnhancedCLICommand(input string, senderID string) {
	// Only process commands from local user
	if senderID != en.ID {
		return
	}

	// Enhanced commands
	switch {
	case strings.HasPrefix(input, "/sendfile "):
		en.fileManager.HandleCLICommand(input)

	case strings.HasPrefix(input, "/voice "):
		en.voiceManager.HandleCLICommand(input)

	case strings.HasPrefix(input, "/help"):
		en.showEnhancedHelp()

	case strings.HasPrefix(input, "/"):
		// Other commands - pass to original CLI handler
		en.handleCLIInput(input)

	default:
		// Regular message - send encrypted
		if err := en.SendEncryptedText(input); err != nil {
			log.Printf("Failed to send encrypted message: %v", err)
			return
		}

		// Also send to UI
		if en.uiChannel != nil {
			en.uiChannel <- Message{
				SenderID: en.ID,
				Content:  []byte(input),
			}
		}
	}
}

// handleKeyExchange processes public key exchange
func (en *EnhancedNode) handleKeyExchange(peerID string, keyData []byte) {
	// Add peer's public key using the peer ID from the message sender
	// This is crucial because the sender ID is their listen address,
	// not the ephemeral connection port
	if err := en.cryptoManager.AddPeerKey(peerID, string(keyData)); err != nil {
		log.Printf("Failed to add peer key for %s: %v", peerID, err)
	} else {
		log.Printf("✅ Added public key for peer %s", peerID)
	}
}

// sendPublicKey sends our public key to a peer (unencrypted for initial exchange)
func (en *EnhancedNode) sendPublicKey(peerID string) error {
	publicKeyPEM, err := en.cryptoManager.GetPublicKeyPEM()
	if err != nil {
		return err
	}

	// Create a special key exchange message (unencrypted)
	// Format: KEY_EXCHANGE:<base64 encoded public key>
	keyExchangeMsg := Message{
		SenderID: en.ID,
		Content:  []byte(fmt.Sprintf("KEY_EXCHANGE:%s", publicKeyPEM)),
	}

	// Send to peer
	en.peersMutex.RLock()
	peer, exists := en.Peers[peerID]
	en.peersMutex.RUnlock()

	if !exists {
		return fmt.Errorf("peer %s not connected", peerID)
	}

	// Serialize the message
	networkMsg := fmt.Sprintf("%s%c%s", keyExchangeMsg.SenderID, delimiter, string(keyExchangeMsg.Content))

	select {
	case peer.Send <- []byte(networkMsg):
		log.Printf("Sent public key to peer %s", peerID)
		return nil
	default:
		return fmt.Errorf("peer send channel full")
	}
}

// broadcastEncrypted broadcasts an encrypted message to all peers
func (en *EnhancedNode) broadcastEncrypted(plaintext []byte, msgType string) error {
	en.peersMutex.RLock()
	defer en.peersMutex.RUnlock()

	var lastError error
	for peerID, peer := range en.Peers {
		// Get the actual node ID (listen address) for encryption
		// The peerID here is the connection address (ephemeral port)
		// But we need the node's listen address for key lookup
		en.peerIDMapLock.RLock()
		actualNodeID, exists := en.peerIDMap[peerID]
		en.peerIDMapLock.RUnlock()

		if !exists {
			// If we haven't received a message from this peer yet, skip encryption
			log.Printf("Skipping encryption for %s: no node ID mapping yet", peerID)
			continue
		}

		// Encrypt message for this peer using their actual node ID
		encryptedMsg, err := en.cryptoManager.EncryptMessage(actualNodeID, plaintext, msgType)
		if err != nil {
			log.Printf("Failed to encrypt message for %s (%s): %v", peerID, actualNodeID, err)
			lastError = err
			continue
		}

		// Serialize encrypted message
		encryptedData, err := json.Marshal(encryptedMsg)
		if err != nil {
			log.Printf("Failed to serialize message for %s: %v", peerID, err)
			lastError = err
			continue
		}

		// Send to peer
		select {
		case peer.Send <- encryptedData:
			// Message sent successfully
		default:
			log.Printf("Failed to send message to %s: channel full", peerID)
			lastError = fmt.Errorf("channel full for %s", peerID)
		}
	}

	return lastError
}

// SendEncryptedText sends an encrypted text message to all peers
func (en *EnhancedNode) SendEncryptedText(text string) error {
	return en.broadcastEncrypted([]byte(text), "text")
}

// showEnhancedHelp displays enhanced command help
func (en *EnhancedNode) showEnhancedHelp() {
	helpText := `Enhanced Commands:

📁 File Sharing:
  /sendfile <peer> <file_path> - Send file to specific peer

🎙️ Voice Messages:
  /voice <duration> - Record and send voice message (1-60 seconds)

🔒 Encryption:
  All messages are automatically encrypted

📋 Standard Commands:
  /connect <addr> - Connect to peer
  /peers - List connected peers
  /discovered - List discovered peers
  /quit - Exit application
`

	if en.uiChannel != nil {
		en.uiChannel <- Message{
			SenderID: "System",
			Content:  []byte(helpText),
		}
	} else {
		fmt.Println(helpText)
	}
}

// StartEnhanced starts the enhanced node with all features
func (en *EnhancedNode) StartEnhanced() {
	log.Printf("Starting enhanced P2P chat node %s", en.ID)
	log.Printf("Features: 🔒 Encryption | 📁 File Sharing | 🎙️ Voice Messages")
	fmt.Println("Commands: /help for help, /quit to exit")

	// Start the base node
	en.wg.Add(1)
	go en.handleServer()

	en.wg.Add(1)
	go en.handleCLI()

	en.wg.Add(1)
	go en.gossipPeerList()

	if en.discoveryConn != nil {
		en.wg.Add(1)
		go en.handleDiscovery()

		en.wg.Add(1)
		go en.announcePresence()
	}

	// Start message processing
	en.wg.Add(1)
	go func() {
		defer en.wg.Done()
		for {
			select {
			case peer := <-en.NewPeer:
				en.addPeer(peer)
				// Send public key to new peer
				go en.sendPublicKey(peer.ID)

			case peerID := <-en.RemovePeer:
				en.removePeer(peerID)

			case msg := <-en.IncomingMsg:
				// Handle incoming messages (no race condition now)
				en.handleIncomingMessage(msg)

			case input := <-en.CLIInput:
				en.handleEnhancedCLICommand(input, en.ID)

			case peerAddr := <-en.DiscoveredPeer:
				en.handleDiscoveredPeer(peerAddr)

			case peerList := <-en.PeerListGossip:
				en.handlePeerListGossip(peerList)

			case <-en.Shutdown:
				return
			}
		}
	}()

	// Wait for shutdown
	en.wg.Wait()
	log.Printf("Enhanced node %s shutdown complete", en.ID)
}
