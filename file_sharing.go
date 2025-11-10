package main

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	chunkSize = 8192 // 8KB chunks
)

// FileTransferManager manages all file transfers
type FileTransferManager struct {
	mutex           sync.RWMutex
	activeTransfers map[string]*FileTransfer
	crypto          *CryptoManager
	node            *Node
	fileDir         string
}

// FileTransfer represents an active file transfer
type FileTransfer struct {
	FileID      string
	FileName    string
	FileSize    int64
	Chunks      map[int][]byte
	TotalChunks int
	Status      string // "pending", "active", "complete", "failed"
	Progress    int
	mutex       sync.Mutex
	PeerID      string
	IsOutgoing  bool
	FilePath    string // For outgoing transfers
}

// FileMessage represents a file transfer message
type FileMessage struct {
	Type        string `json:"type"`         // "request", "accept", "reject", "chunk", "complete"
	FileID      string `json:"file_id"`      // Unique identifier for this transfer
	FileName    string `json:"file_name"`    // Name of the file
	FileSize    int64  `json:"file_size"`    // Total size in bytes
	ChunkIndex  int    `json:"chunk_index"`  // Index of this chunk
	TotalChunks int    `json:"total_chunks"` // Total number of chunks
	Data        string `json:"data"`         // Base64 encoded chunk data
	Checksum    string `json:"checksum"`     // MD5 checksum
}

// NewFileTransferManager creates a new file transfer manager
func NewFileTransferManager(node *Node, crypto *CryptoManager, fileDir string) *FileTransferManager {
	if err := os.MkdirAll(fileDir, 0755); err != nil {
		log.Printf("Warning: Failed to create file directory: %v", err)
	}

	return &FileTransferManager{
		activeTransfers: make(map[string]*FileTransfer),
		crypto:          crypto,
		node:            node,
		fileDir:         fileDir,
	}
}

// SendFile initiates a file transfer
func (ftm *FileTransferManager) SendFile(peerID, filePath string) error {
	// Read file
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Generate file ID
	fileID := generateFileID()
	fileName := filepath.Base(filePath)

	// Create transfer record
	chunks := splitIntoChunks(fileData)
	transfer := &FileTransfer{
		FileID:      fileID,
		FileName:    fileName,
		FileSize:    int64(len(fileData)),
		Chunks:      chunks,
		TotalChunks: len(chunks),
		Status:      "pending",
		Progress:    0,
		PeerID:      peerID,
		IsOutgoing:  true,
		FilePath:    filePath,
	}

	// Store transfer
	ftm.mutex.Lock()
	ftm.activeTransfers[fileID] = transfer
	ftm.mutex.Unlock()

	// Send request message
	requestMsg := FileMessage{
		Type:        "request",
		FileID:      fileID,
		FileName:    fileName,
		FileSize:    int64(len(fileData)),
		TotalChunks: len(chunks),
	}

	if err := ftm.sendFileMessage(peerID, requestMsg); err != nil {
		// Cleanup on error
		ftm.mutex.Lock()
		delete(ftm.activeTransfers, fileID)
		ftm.mutex.Unlock()

		// Notify UI of failure
		if ftm.node.uiChannel != nil {
			ftm.node.uiChannel <- Message{
				SenderID: "SYSTEM",
				Content:  []byte(fmt.Sprintf("Failed to send file request to %s: %v", peerID, err)),
			}
		}
		return fmt.Errorf("failed to send file request: %w", err)
	}

	log.Printf("File transfer request sent: %s (%d bytes)", fileName, len(fileData))
	return nil
}

// HandleFileMessage routes file messages based on type
func (ftm *FileTransferManager) HandleFileMessage(peerID string, fileMsg FileMessage) {
	switch fileMsg.Type {
	case "request":
		ftm.handleFileRequest(peerID, fileMsg)
	case "accept":
		ftm.handleFileAccept(peerID, fileMsg)
	case "reject":
		ftm.handleFileReject(peerID, fileMsg)
	case "chunk":
		ftm.handleFileChunk(peerID, fileMsg)
	case "complete":
		ftm.handleFileComplete(peerID, fileMsg)
	default:
		log.Printf("Unknown file message type: %s", fileMsg.Type)
	}
}

// handleFileRequest handles incoming file transfer requests
func (ftm *FileTransferManager) handleFileRequest(peerID string, fileMsg FileMessage) {
	log.Printf("Received file transfer request from %s: %s (%d bytes)",
		peerID, fileMsg.FileName, fileMsg.FileSize)

	// Auto-accept and create transfer record
	transfer := &FileTransfer{
		FileID:      fileMsg.FileID,
		FileName:    fileMsg.FileName,
		FileSize:    fileMsg.FileSize,
		Chunks:      make(map[int][]byte),
		TotalChunks: fileMsg.TotalChunks,
		Status:      "active",
		Progress:    0,
		PeerID:      peerID,
		IsOutgoing:  false,
	}

	ftm.mutex.Lock()
	ftm.activeTransfers[fileMsg.FileID] = transfer
	ftm.mutex.Unlock()

	// Send accept message
	acceptMsg := FileMessage{
		Type:   "accept",
		FileID: fileMsg.FileID,
	}

	if err := ftm.sendFileMessage(peerID, acceptMsg); err != nil {
		log.Printf("Failed to send accept message: %v", err)
		return
	}

	// Notify UI
	if ftm.node.uiChannel != nil {
		ftm.node.uiChannel <- Message{
			SenderID: "SYSTEM",
			Content:  []byte(fmt.Sprintf("Receiving file from %s: %s (%d bytes)", peerID, fileMsg.FileName, fileMsg.FileSize)),
		}
	}
}

// handleFileAccept handles file transfer acceptance
func (ftm *FileTransferManager) handleFileAccept(peerID string, fileMsg FileMessage) {
	ftm.mutex.RLock()
	transfer, exists := ftm.activeTransfers[fileMsg.FileID]
	ftm.mutex.RUnlock()

	if !exists {
		log.Printf("Unknown file transfer ID: %s", fileMsg.FileID)
		return
	}

	transfer.mutex.Lock()
	transfer.Status = "active"
	transfer.mutex.Unlock()

	log.Printf("File transfer accepted by %s, starting transfer", peerID)

	// Start sending chunks in a goroutine
	go ftm.sendFileChunks(peerID, transfer)
}

// handleFileReject handles file transfer rejection
func (ftm *FileTransferManager) handleFileReject(peerID string, fileMsg FileMessage) {
	ftm.mutex.Lock()
	transfer, exists := ftm.activeTransfers[fileMsg.FileID]
	if exists {
		transfer.mutex.Lock()
		transfer.Status = "failed"
		transfer.mutex.Unlock()
		delete(ftm.activeTransfers, fileMsg.FileID)
	}
	ftm.mutex.Unlock()

	log.Printf("File transfer rejected by %s", peerID)

	// Notify UI
	if ftm.node.uiChannel != nil {
		ftm.node.uiChannel <- Message{
			SenderID: "SYSTEM",
			Content:  []byte(fmt.Sprintf("File transfer rejected by %s", peerID)),
		}
	}
}

// sendFileChunks sends all chunks of a file
func (ftm *FileTransferManager) sendFileChunks(peerID string, transfer *FileTransfer) {
	for i := 0; i < transfer.TotalChunks; i++ {
		transfer.mutex.Lock()
		chunkData := transfer.Chunks[i]
		transfer.mutex.Unlock()

		// Calculate checksum for this chunk
		checksum := fmt.Sprintf("%x", md5.Sum(chunkData))

		chunkMsg := FileMessage{
			Type:        "chunk",
			FileID:      transfer.FileID,
			ChunkIndex:  i,
			TotalChunks: transfer.TotalChunks,
			Data:        base64.StdEncoding.EncodeToString(chunkData),
			Checksum:    checksum,
		}

		if err := ftm.sendFileMessage(peerID, chunkMsg); err != nil {
			log.Printf("Failed to send chunk %d: %v", i, err)
			transfer.mutex.Lock()
			transfer.Status = "failed"
			transfer.mutex.Unlock()

			// Cleanup on error
			ftm.mutex.Lock()
			delete(ftm.activeTransfers, transfer.FileID)
			ftm.mutex.Unlock()

			// Notify UI of failure
			if ftm.node.uiChannel != nil {
				ftm.node.uiChannel <- Message{
					SenderID: "SYSTEM",
					Content:  []byte(fmt.Sprintf("Failed to send file chunk to %s: %v", peerID, err)),
				}
			}
			return
		}

		// Update progress
		transfer.mutex.Lock()
		transfer.Progress = ((i + 1) * 100) / transfer.TotalChunks
		transfer.mutex.Unlock()

		// Small delay between chunks to avoid overwhelming the network
		time.Sleep(10 * time.Millisecond)
	}

	// Send complete message
	completeMsg := FileMessage{
		Type:   "complete",
		FileID: transfer.FileID,
	}

	if err := ftm.sendFileMessage(peerID, completeMsg); err != nil {
		log.Printf("Failed to send complete message: %v", err)
		return
	}

	transfer.mutex.Lock()
	transfer.Status = "complete"
	transfer.mutex.Unlock()

	log.Printf("File transfer complete: %s", transfer.FileName)

	// Notify UI
	if ftm.node.uiChannel != nil {
		ftm.node.uiChannel <- Message{
			SenderID: "SYSTEM",
			Content:  []byte(fmt.Sprintf("File sent successfully: %s", transfer.FileName)),
		}
	}

	// Clean up after successful transfer
	ftm.mutex.Lock()
	delete(ftm.activeTransfers, transfer.FileID)
	ftm.mutex.Unlock()
}

// handleFileChunk receives and validates file chunks
func (ftm *FileTransferManager) handleFileChunk(peerID string, fileMsg FileMessage) {
	ftm.mutex.RLock()
	transfer, exists := ftm.activeTransfers[fileMsg.FileID]
	ftm.mutex.RUnlock()

	if !exists {
		log.Printf("Unknown file transfer ID: %s", fileMsg.FileID)
		return
	}

	// Decode chunk data
	chunkData, err := base64.StdEncoding.DecodeString(fileMsg.Data)
	if err != nil {
		log.Printf("Failed to decode chunk data: %v", err)
		return
	}

	// Validate checksum
	checksum := fmt.Sprintf("%x", md5.Sum(chunkData))
	if checksum != fileMsg.Checksum {
		log.Printf("Checksum mismatch for chunk %d", fileMsg.ChunkIndex)
		return
	}

	// Store chunk
	transfer.mutex.Lock()
	transfer.Chunks[fileMsg.ChunkIndex] = chunkData
	transfer.Progress = (len(transfer.Chunks) * 100) / transfer.TotalChunks
	transfer.mutex.Unlock()

	log.Printf("Received chunk %d/%d (%d%%)", fileMsg.ChunkIndex+1, fileMsg.TotalChunks, transfer.Progress)
}

// handleFileComplete assembles and saves the complete file
func (ftm *FileTransferManager) handleFileComplete(peerID string, fileMsg FileMessage) {
	ftm.mutex.RLock()
	transfer, exists := ftm.activeTransfers[fileMsg.FileID]
	ftm.mutex.RUnlock()

	if !exists {
		log.Printf("Unknown file transfer ID: %s", fileMsg.FileID)
		return
	}

	transfer.mutex.Lock()
	defer transfer.mutex.Unlock()

	// Check if we have all chunks
	if len(transfer.Chunks) != transfer.TotalChunks {
		log.Printf("Incomplete file: have %d chunks, expected %d", len(transfer.Chunks), transfer.TotalChunks)
		transfer.Status = "failed"
		return
	}

	// Assemble file
	var fileData []byte
	for i := 0; i < transfer.TotalChunks; i++ {
		chunk, exists := transfer.Chunks[i]
		if !exists {
			log.Printf("Missing chunk %d", i)
			transfer.Status = "failed"
			return
		}
		fileData = append(fileData, chunk...)
	}

	// Save file to downloads directory
	downloadsDir := "downloads"
	if err := os.MkdirAll(downloadsDir, 0755); err != nil {
		log.Printf("Failed to create downloads directory: %v", err)
		transfer.Status = "failed"
		return
	}

	filePath := filepath.Join(downloadsDir, transfer.FileName)
	if err := os.WriteFile(filePath, fileData, 0644); err != nil {
		log.Printf("Failed to save file: %v", err)
		transfer.Status = "failed"
		return
	}

	transfer.Status = "complete"
	log.Printf("File received successfully: %s (%d bytes)", transfer.FileName, len(fileData))

	// Notify UI
	if ftm.node.uiChannel != nil {
		ftm.node.uiChannel <- Message{
			SenderID: "SYSTEM",
			Content:  []byte(fmt.Sprintf("File received successfully: %s (saved to %s)", transfer.FileName, filePath)),
		}
	}

	// Clean up
	ftm.mutex.Lock()
	delete(ftm.activeTransfers, fileMsg.FileID)
	ftm.mutex.Unlock()
}

// sendFileMessage encrypts and sends a file message to a peer
func (ftm *FileTransferManager) sendFileMessage(peerID string, fileMsg FileMessage) error {
	// Serialise file message
	msgData, err := json.Marshal(fileMsg)
	if err != nil {
		return fmt.Errorf("failed to serialise file message: %w", err)
	}

	// Encrypt message
	encryptedMsg, err := ftm.crypto.EncryptMessage(peerID, msgData, "file")
	if err != nil {
		return fmt.Errorf("failed to encrypt file message: %w", err)
	}

	// Serialise encrypted message
	encryptedData, err := json.Marshal(encryptedMsg)
	if err != nil {
		return fmt.Errorf("failed to serialise encrypted message: %w", err)
	}

	// Get peer connection
	ftm.node.peersMutex.RLock()
	peer, exists := ftm.node.Peers[peerID]
	ftm.node.peersMutex.RUnlock()

	if !exists {
		return fmt.Errorf("peer not found: %s", peerID)
	}

	// Send to peer
	networkMsg := fmt.Sprintf("%s|%s", ftm.node.ID, string(encryptedData))
	select {
	case peer.Send <- []byte(networkMsg):
		return nil
	default:
		return fmt.Errorf("peer send channel full")
	}
}

// HandleCLICommand parses and handles file sharing CLI commands
func (ftm *FileTransferManager) HandleCLICommand(command string) {
	parts := strings.Fields(command)
	if len(parts) < 3 {
		log.Println("Usage: /sendfile <peer_id> <file_path>")
		return
	}

	peerID := parts[1]
	filePath := strings.Join(parts[2:], " ")

	if err := ftm.SendFile(peerID, filePath); err != nil {
		log.Printf("Failed to send file: %v", err)
		if ftm.node.uiChannel != nil {
			ftm.node.uiChannel <- Message{
				SenderID: "System",
				Content:  []byte(fmt.Sprintf("❌ Failed to send file: %v", err)),
			}
		}
	}
}

// generateFileID generates a unique file transfer ID
func generateFileID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// splitIntoChunks splits data into chunks
func splitIntoChunks(data []byte) map[int][]byte {
	chunks := make(map[int][]byte)
	totalSize := len(data)
	chunkIndex := 0

	for offset := 0; offset < totalSize; offset += chunkSize {
		end := offset + chunkSize
		if end > totalSize {
			end = totalSize
		}
		chunks[chunkIndex] = data[offset:end]
		chunkIndex++
	}

	return chunks
}
