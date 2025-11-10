package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/faiface/beep"
	"github.com/faiface/beep/mp3"
	"github.com/faiface/beep/speaker"
	"github.com/faiface/beep/wav"
)

// VoiceMessageManager handles voice recording and playback
type VoiceMessageManager struct {
	node            *Node
	crypto          *CryptoManager
	isRecording     bool
	recordMutex     sync.Mutex
	voiceDir        string
	speakerInitOnce sync.Once
	speakerInitErr  error
}

// VoiceMessage represents a voice message
type VoiceMessage struct {
	Type       string `json:"type"`
	AudioData  string `json:"audio_data"` // base64 encoded
	Duration   int    `json:"duration"`   // in seconds
	SampleRate int    `json:"sample_rate"`
	Format     string `json:"format"` // "mp3" or "wav"
}

// NewVoiceMessageManager creates a new voice message manager
func NewVoiceMessageManager(node *Node, crypto *CryptoManager, voiceDir string) *VoiceMessageManager {
	if err := os.MkdirAll(voiceDir, 0755); err != nil {
		log.Printf("Warning: Failed to create voice directory: %v", err)
	}

	return &VoiceMessageManager{
		node:        node,
		crypto:      crypto,
		isRecording: false,
		voiceDir:    voiceDir,
	}
}

// RecordVoiceMessage records a voice message and broadcasts it
func (vm *VoiceMessageManager) RecordVoiceMessage(durationStr string) error {
	vm.recordMutex.Lock()
	if vm.isRecording {
		vm.recordMutex.Unlock()
		return fmt.Errorf("already recording")
	}
	vm.isRecording = true
	vm.recordMutex.Unlock()

	defer func() {
		vm.recordMutex.Lock()
		vm.isRecording = false
		vm.recordMutex.Unlock()
	}()

	// Parse duration
	duration, err := strconv.Atoi(durationStr)
	if err != nil || duration <= 0 || duration > 60 {
		return fmt.Errorf("invalid duration: must be between 1 and 60 seconds")
	}

	log.Printf("Recording voice message for %d seconds...", duration)

	// Record audio to WAV
	wavPath := filepath.Join(vm.voiceDir, fmt.Sprintf("recording_%d.wav", time.Now().Unix()))
	if err := vm.recordAudio(wavPath, duration); err != nil {
		return fmt.Errorf("failed to record audio: %w", err)
	}
	defer os.Remove(wavPath)

	// Convert to MP3
	mp3Path := filepath.Join(vm.voiceDir, fmt.Sprintf("recording_%d.mp3", time.Now().Unix()))
	if err := vm.convertToMP3(wavPath, mp3Path); err != nil {
		return fmt.Errorf("failed to convert to MP3: %w", err)
	}
	defer os.Remove(mp3Path)

	// Read MP3 file
	audioData, err := os.ReadFile(mp3Path)
	if err != nil {
		return fmt.Errorf("failed to read MP3 file: %w", err)
	}

	// Create voice message
	voiceMsg := VoiceMessage{
		Type:       "voice",
		AudioData:  base64.StdEncoding.EncodeToString(audioData),
		Duration:   duration,
		SampleRate: 44100,
		Format:     "mp3",
	}

	// Broadcast voice message
	if err := vm.broadcastVoiceMessage(voiceMsg); err != nil {
		return fmt.Errorf("failed to broadcast voice message: %w", err)
	}

	log.Println("Voice message recorded and sent successfully")
	return nil
}

// HandleVoiceMessage processes incoming voice messages
func (vm *VoiceMessageManager) HandleVoiceMessage(senderID string, voiceMsg VoiceMessage) {
	log.Printf("Received voice message from %s (duration: %d seconds)", senderID, voiceMsg.Duration)

	// Decode audio data
	audioData, err := base64.StdEncoding.DecodeString(voiceMsg.AudioData)
	if err != nil {
		log.Printf("Failed to decode audio data: %v", err)
		return
	}

	// Play the voice message
	if err := vm.playVoiceMessage(audioData, voiceMsg.Format); err != nil {
		log.Printf("Failed to play voice message: %v", err)
		return
	}

	// Notify UI
	if vm.node.uiChannel != nil {
		vm.node.uiChannel <- Message{
			SenderID: "System",
			Content:  []byte(fmt.Sprintf("🔊 Played voice message from %s", senderID)),
		}
	}
}

// recordAudio records audio using ffmpeg with platform-specific settings
func (vm *VoiceMessageManager) recordAudio(outputPath string, duration int) error {
	var args []string

	// Platform-specific audio input configuration
	switch runtime.GOOS {
	case "windows":
		args = []string{
			"-f", "dshow",
			"-i", "audio=Microphone",
			"-t", strconv.Itoa(duration),
			"-ar", "44100",
			"-ac", "1",
			outputPath,
		}
	case "darwin":
		args = []string{
			"-f", "avfoundation",
			"-i", ":0",
			"-t", strconv.Itoa(duration),
			"-ar", "44100",
			"-ac", "1",
			outputPath,
		}
	case "linux":
		args = []string{
			"-f", "pulse",
			"-i", "default",
			"-t", strconv.Itoa(duration),
			"-ar", "44100",
			"-ac", "1",
			outputPath,
		}
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	cmd := exec.Command("ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg recording failed: %w, stderr: %s", err, stderr.String())
	}

	return nil
}

// convertToMP3 converts a WAV file to MP3 using ffmpeg
func (vm *VoiceMessageManager) convertToMP3(wavPath, mp3Path string) error {
	cmd := exec.Command("ffmpeg",
		"-i", wavPath,
		"-codec:a", "libmp3lame",
		"-qscale:a", "2",
		mp3Path,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg conversion failed: %w, stderr: %s", err, stderr.String())
	}

	return nil
}

// playVoiceMessage plays a voice message using the beep library
func (vm *VoiceMessageManager) playVoiceMessage(audioData []byte, format string) error {
	// Initialise speaker once
	vm.speakerInitOnce.Do(func() {
		sampleRate := beep.SampleRate(44100)
		vm.speakerInitErr = speaker.Init(sampleRate, sampleRate.N(time.Second/10))
	})

	if vm.speakerInitErr != nil {
		return fmt.Errorf("failed to initialise speaker: %w", vm.speakerInitErr)
	}

	// Create a reader from audio data
	reader := io.NopCloser(bytes.NewReader(audioData))

	var streamer beep.StreamSeekCloser
	var streamFormat beep.Format
	var err error

	switch format {
	case "mp3":
		streamer, streamFormat, err = mp3.Decode(reader)
	case "wav":
		streamer, streamFormat, err = wav.Decode(reader)
	default:
		return fmt.Errorf("unsupported audio format: %s", format)
	}

	if err != nil {
		return fmt.Errorf("failed to decode audio: %w", err)
	}
	defer streamer.Close()

	// Resample if necessary
	resampled := beep.Resample(4, streamFormat.SampleRate, beep.SampleRate(44100), streamer)

	// Play audio
	done := make(chan bool)
	speaker.Play(beep.Seq(resampled, beep.Callback(func() {
		done <- true
	})))

	<-done
	log.Println("Voice message playback completed")
	return nil
}

// broadcastVoiceMessage encrypts and sends a voice message to all peers
func (vm *VoiceMessageManager) broadcastVoiceMessage(voiceMsg VoiceMessage) error {
	// Marshal voice message
	data, err := json.Marshal(voiceMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal voice message: %w", err)
	}

	// Broadcast to all connected peers
	vm.node.peersMutex.RLock()
	defer vm.node.peersMutex.RUnlock()

	var lastError error
	for peerID, peer := range vm.node.Peers {
		// Encrypt message for this specific peer
		encryptedMsg, err := vm.crypto.EncryptMessage(peerID, data, "voice")
		if err != nil {
			log.Printf("Failed to encrypt voice message for %s: %v", peerID, err)
			lastError = err
			continue
		}

		// Serialize encrypted message
		encryptedData, err := json.Marshal(encryptedMsg)
		if err != nil {
			log.Printf("Failed to serialize voice message for %s: %v", peerID, err)
			lastError = err
			continue
		}

		// Format as network message
		networkMsg := fmt.Sprintf("%s%c%s", vm.node.ID, delimiter, string(encryptedData))

		// Send to peer
		select {
		case peer.Send <- []byte(networkMsg):
			// Message sent successfully
		default:
			log.Printf("Failed to send voice message to %s: channel full", peerID)
			lastError = fmt.Errorf("channel full for %s", peerID)
		}
	}

	return lastError
}

// HandleCLICommand processes voice-related CLI commands
func (vm *VoiceMessageManager) HandleCLICommand(command string) {
	parts := strings.Fields(command)
	if len(parts) < 2 {
		log.Println("Usage: /voice <duration_in_seconds> (1-60)")
		return
	}

	durationStr := parts[1]
	if err := vm.RecordVoiceMessage(durationStr); err != nil {
		log.Printf("Failed to record voice message: %v", err)
		if vm.node.uiChannel != nil {
			vm.node.uiChannel <- Message{
				SenderID: "System",
				Content:  []byte(fmt.Sprintf("❌ Failed to record voice message: %v", err)),
			}
		}
	}
}
