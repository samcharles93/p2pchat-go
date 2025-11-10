package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CryptoManager handles encryption and key management
type CryptoManager struct {
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	peerKeys   map[string]*rsa.PublicKey
	keysMutex  sync.RWMutex
	keysDir    string
}

// EncryptedMessage represents an encrypted message with metadata
type EncryptedMessage struct {
	Ciphertext   string `json:"ciphertext"`
	Signature    string `json:"signature"`
	SenderPubKey string `json:"sender_pubkey"`
	Timestamp    int64  `json:"timestamp"`
	MessageType  string `json:"message_type"`
}

// NewCryptoManager creates a new crypto manager
func NewCryptoManager(keysDir string) (*CryptoManager, error) {
	if err := os.MkdirAll(keysDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create keys directory: %w", err)
	}

	cm := &CryptoManager{
		peerKeys: make(map[string]*rsa.PublicKey),
		keysDir:  keysDir,
	}

	// Try to load existing keys
	privatePath := filepath.Join(keysDir, "private.pem")
	publicPath := filepath.Join(keysDir, "public.pem")

	if _, err := os.Stat(privatePath); err == nil {
		// Keys exist, load them
		if err := cm.loadKeys(privatePath, publicPath); err != nil {
			return nil, fmt.Errorf("failed to load existing keys: %w", err)
		}
	} else {
		// Generate new keys
		if err := cm.generateKeys(); err != nil {
			return nil, fmt.Errorf("failed to generate keys: %w", err)
		}

		// Save keys
		if err := cm.saveKeys(privatePath, publicPath); err != nil {
			return nil, fmt.Errorf("failed to save keys: %w", err)
		}
	}

	return cm, nil
}

// generateKeys generates a new RSA key pair
func (cm *CryptoManager) generateKeys() error {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	cm.privateKey = privateKey
	cm.publicKey = &privateKey.PublicKey
	return nil
}

// saveKeys saves the key pair to files
func (cm *CryptoManager) saveKeys(privatePath, publicPath string) error {
	// Save private key
	privateBytes := x509.MarshalPKCS1PrivateKey(cm.privateKey)
	privatePEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateBytes,
	})

	if err := os.WriteFile(privatePath, privatePEM, 0600); err != nil {
		return err
	}

	// Save public key
	publicBytes, err := x509.MarshalPKIXPublicKey(cm.publicKey)
	if err != nil {
		return err
	}

	publicPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicBytes,
	})

	return os.WriteFile(publicPath, publicPEM, 0644)
}

// loadKeys attempts to load existing keys from files
func (cm *CryptoManager) loadKeys(privatePath, publicPath string) error {
	// Load private key
	privateData, err := os.ReadFile(privatePath)
	if err != nil {
		return err
	}

	block, _ := pem.Decode(privateData)
	if block == nil {
		return errors.New("failed to decode private key PEM")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse private key: %w", err)
	}

	// Load public key
	publicData, err := os.ReadFile(publicPath)
	if err != nil {
		return err
	}

	block, _ = pem.Decode(publicData)
	if block == nil {
		return errors.New("failed to decode public key PEM")
	}

	publicKeyInterface, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse public key: %w", err)
	}

	publicKey, ok := publicKeyInterface.(*rsa.PublicKey)
	if !ok {
		return errors.New("not an RSA public key")
	}

	cm.privateKey = privateKey
	cm.publicKey = publicKey
	return nil
}

// GetPublicKeyPEM returns the public key in PEM format
func (cm *CryptoManager) GetPublicKeyPEM() (string, error) {
	publicBytes, err := x509.MarshalPKIXPublicKey(cm.publicKey)
	if err != nil {
		return "", err
	}

	publicPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicBytes,
	})

	return string(publicPEM), nil
}

// AddPeerKey adds a peer's public key
func (cm *CryptoManager) AddPeerKey(peerID string, publicKeyPEM string) error {
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		return errors.New("failed to decode peer public key PEM")
	}

	publicKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse peer public key: %w", err)
	}

	rsaPublicKey, ok := publicKey.(*rsa.PublicKey)
	if !ok {
		return errors.New("peer public key is not RSA")
	}

	cm.keysMutex.Lock()
	defer cm.keysMutex.Unlock()
	cm.peerKeys[peerID] = rsaPublicKey

	return nil
}

// EncryptMessage encrypts and signs a message for a specific peer
func (cm *CryptoManager) EncryptMessage(peerID string, plaintext []byte, messageType string) (*EncryptedMessage, error) {
	cm.keysMutex.RLock()
	peerPublicKey, exists := cm.peerKeys[peerID]
	cm.keysMutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no public key for peer: %s", peerID)
	}

	// Encrypt with peer's public key
	ciphertext, err := rsa.EncryptOAEP(
		sha256.New(),
		rand.Reader,
		peerPublicKey,
		plaintext,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("encryption failed: %w", err)
	}

	// Sign with our private key
	hash := sha256.Sum256(plaintext)
	signature, err := rsa.SignPKCS1v15(rand.Reader, cm.privateKey, crypto.SHA256, hash[:])
	if err != nil {
		return nil, fmt.Errorf("signing failed: %w", err)
	}

	// Get our public key for verification
	publicKeyPEM, err := cm.GetPublicKeyPEM()
	if err != nil {
		return nil, err
	}

	return &EncryptedMessage{
		Ciphertext:   base64.StdEncoding.EncodeToString(ciphertext),
		Signature:    base64.StdEncoding.EncodeToString(signature),
		SenderPubKey: publicKeyPEM,
		Timestamp:    time.Now().Unix(),
		MessageType:  messageType,
	}, nil
}

// DecryptMessage decrypts and verifies a message
func (cm *CryptoManager) DecryptMessage(encMsg *EncryptedMessage) ([]byte, string, error) {
	// Decode ciphertext
	ciphertext, err := base64.StdEncoding.DecodeString(encMsg.Ciphertext)
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	// Decrypt with our private key
	plaintext, err := rsa.DecryptOAEP(
		sha256.New(),
		rand.Reader,
		cm.privateKey,
		ciphertext,
		nil,
	)
	if err != nil {
		return nil, "", fmt.Errorf("decryption failed: %w", err)
	}

	// Decode signature
	signature, err := base64.StdEncoding.DecodeString(encMsg.Signature)
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode signature: %w", err)
	}

	// Parse sender's public key
	block, _ := pem.Decode([]byte(encMsg.SenderPubKey))
	if block == nil {
		return nil, "", errors.New("failed to decode sender public key")
	}

	senderPublicKeyInterface, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse sender public key: %w", err)
	}

	senderPublicKey, ok := senderPublicKeyInterface.(*rsa.PublicKey)
	if !ok {
		return nil, "", errors.New("sender public key is not RSA")
	}

	// Verify signature
	hash := sha256.Sum256(plaintext)
	if err := rsa.VerifyPKCS1v15(senderPublicKey, crypto.SHA256, hash[:], signature); err != nil {
		return nil, "", fmt.Errorf("signature verification failed: %w", err)
	}

	return plaintext, encMsg.MessageType, nil
}
