// Package privacy - Chain Key Derivation (Method B) implementation
package privacy

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ChainNode represents a node in the key derivation chain
type ChainNode struct {
	ID            string `json:"id"`
	Index         int    `json:"index"`
	DerivedKey    []byte `json:"-"` // Never serialize the actual key
	KeyHash       string `json:"key_hash"`
	NextKeyHint   string `json:"next_key_hint"`
	IntegrityHash string `json:"integrity_hash"`
	CreatedAt     int64  `json:"created_at"`
	ExpiresAt     int64  `json:"expires_at,omitempty"`
}

// ChainKeyDerivation implements Method B: Chain-based dynamic key derivation
type ChainKeyDerivation struct {
	masterKey    []byte
	chainNodes   map[string]*ChainNode
	currentIndex int
	mu           sync.RWMutex
}

// ChainEncryptedData represents data encrypted with chain key derivation
type ChainEncryptedData struct {
	NodeID        string `json:"node_id"`
	Ciphertext    string `json:"ciphertext"`
	IV            string `json:"iv"`
	IntegrityHash string `json:"integrity_hash"`
	Algorithm     string `json:"algorithm"`
}

// NewChainKeyDerivation creates a new chain key derivation instance
func NewChainKeyDerivation(masterPassword string) (*ChainKeyDerivation, error) {
	// Derive master key from password using PBKDF2
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}

	masterKey := deriveMasterKey(masterPassword, salt)

	ckd := &ChainKeyDerivation{
		masterKey:    masterKey,
		chainNodes:   make(map[string]*ChainNode),
		currentIndex: 0,
	}

	// Initialize the first node in the chain
	_, err := ckd.deriveNextNode()
	if err != nil {
		return nil, fmt.Errorf("initialize chain: %w", err)
	}

	return ckd, nil
}

// deriveMasterKey derives the master key from password
func deriveMasterKey(password string, salt []byte) []byte {
	// Use HMAC-SHA256 for key derivation
	h := hmac.New(sha256.New, salt)
	h.Write([]byte(password))
	return h.Sum(nil)
}

// deriveNextNode derives the next node in the chain
func (c *ChainKeyDerivation) deriveNextNode() (*ChainNode, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	nodeID := fmt.Sprintf("node_%d_%x", c.currentIndex, randBytes(4))

	// Derive key for this node
	var derivedKey []byte
	if c.currentIndex == 0 {
		// First node: derive from master key
		derivedKey = c.deriveKeyFromMaster(c.currentIndex)
	} else {
		// Subsequent nodes: derive from previous node's key
		prevNode := c.getNodeByIndex(c.currentIndex - 1)
		if prevNode == nil {
			return nil, errors.New("previous node not found")
		}
		derivedKey = c.deriveKeyFromPrevious(prevNode.DerivedKey, c.currentIndex)
	}

	// Calculate key hash (for verification without exposing key)
	keyHash := sha256.Sum256(derivedKey)

	// Calculate next key hint (partial hash for forward security)
	nextKeyHint := c.calculateNextKeyHint(derivedKey, c.currentIndex+1)

	// Calculate integrity hash
	integrityHash := c.calculateIntegrityHash(nodeID, c.currentIndex, keyHash[:])

	node := &ChainNode{
		ID:            nodeID,
		Index:         c.currentIndex,
		DerivedKey:    derivedKey,
		KeyHash:       hex.EncodeToString(keyHash[:]),
		NextKeyHint:   nextKeyHint,
		IntegrityHash: integrityHash,
		CreatedAt:     time.Now().UnixNano(),
	}

	c.chainNodes[nodeID] = node
	c.currentIndex++

	return node, nil
}

// deriveKeyFromMaster derives a key from the master key
func (c *ChainKeyDerivation) deriveKeyFromMaster(index int) []byte {
	h := hmac.New(sha256.New, c.masterKey)
	h.Write([]byte(fmt.Sprintf("chain_node_%d", index)))
	return h.Sum(nil)
}

// deriveKeyFromPrevious derives a key from the previous node's key
func (c *ChainKeyDerivation) deriveKeyFromPrevious(prevKey []byte, index int) []byte {
	h := hmac.New(sha256.New, prevKey)
	h.Write([]byte(fmt.Sprintf("chain_derive_%d", index)))
	return h.Sum(nil)
}

// calculateNextKeyHint calculates a hint for the next key (forward security)
func (c *ChainKeyDerivation) calculateNextKeyHint(currentKey []byte, nextIndex int) string {
	h := hmac.New(sha256.New, currentKey)
	h.Write([]byte(fmt.Sprintf("next_hint_%d", nextIndex)))
	hint := h.Sum(nil)
	return hex.EncodeToString(hint[:8]) // Only first 8 bytes as hint
}

// calculateIntegrityHash calculates integrity hash for a node
func (c *ChainKeyDerivation) calculateIntegrityHash(nodeID string, index int, keyHash []byte) string {
	h := sha256.New()
	h.Write([]byte(nodeID))
	h.Write([]byte(fmt.Sprintf("%d", index)))
	h.Write(keyHash)
	return hex.EncodeToString(h.Sum(nil))
}

// getNodeByIndex returns a node by its index
func (c *ChainKeyDerivation) getNodeByIndex(index int) *ChainNode {
	for _, node := range c.chainNodes {
		if node.Index == index {
			return node
		}
	}
	return nil
}

// GetCurrentNode returns the current (latest) node
func (c *ChainKeyDerivation) GetCurrentNode() *ChainNode {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.getNodeByIndex(c.currentIndex - 1)
}

// GetNode returns a node by ID
func (c *ChainKeyDerivation) GetNode(nodeID string) *ChainNode {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.chainNodes[nodeID]
}

// RotateKey creates a new node in the chain (key rotation)
func (c *ChainKeyDerivation) RotateKey() (*ChainNode, error) {
	return c.deriveNextNode()
}

// Encrypt encrypts data using the current chain node's key
func (c *ChainKeyDerivation) Encrypt(_ context.Context, plaintext string) (*ChainEncryptedData, error) {
	currentNode := c.GetCurrentNode()
	if currentNode == nil {
		return nil, errors.New("no current node available")
	}

	// Generate IV
	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		return nil, fmt.Errorf("generate iv: %w", err)
	}

	// Create cipher
	block, err := aes.NewCipher(currentNode.DerivedKey)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	// PKCS7 padding
	plainBytes := []byte(plaintext)
	paddedPlain := pkcs7Pad(plainBytes, aes.BlockSize)

	// Encrypt
	ciphertext := make([]byte, len(paddedPlain))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, paddedPlain)

	// Calculate integrity hash
	integrityHash := c.calculateDataIntegrityHash(ciphertext, iv, currentNode.ID)

	return &ChainEncryptedData{
		NodeID:        currentNode.ID,
		Ciphertext:    base64.StdEncoding.EncodeToString(ciphertext),
		IV:            base64.StdEncoding.EncodeToString(iv),
		IntegrityHash: integrityHash,
		Algorithm:     "aes-256-cbc-chain",
	}, nil
}

// Decrypt decrypts data using the specified chain node's key
func (c *ChainKeyDerivation) Decrypt(_ context.Context, encrypted *ChainEncryptedData) (string, error) {
	node := c.GetNode(encrypted.NodeID)
	if node == nil {
		return "", fmt.Errorf("node not found: %s", encrypted.NodeID)
	}

	// Decode ciphertext and IV
	ciphertext, err := base64.StdEncoding.DecodeString(encrypted.Ciphertext)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}

	iv, err := base64.StdEncoding.DecodeString(encrypted.IV)
	if err != nil {
		return "", fmt.Errorf("decode iv: %w", err)
	}

	// Verify integrity
	expectedHash := c.calculateDataIntegrityHash(ciphertext, iv, encrypted.NodeID)
	if expectedHash != encrypted.IntegrityHash {
		return "", errors.New("integrity check failed")
	}

	// Create cipher
	block, err := aes.NewCipher(node.DerivedKey)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	// Decrypt
	plaintext := make([]byte, len(ciphertext))
	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(plaintext, ciphertext)

	// Remove padding
	unpaddedPlain, err := pkcs7Unpad(plaintext)
	if err != nil {
		return "", fmt.Errorf("unpad: %w", err)
	}

	return string(unpaddedPlain), nil
}

// calculateDataIntegrityHash calculates integrity hash for encrypted data
func (c *ChainKeyDerivation) calculateDataIntegrityHash(ciphertext, iv []byte, nodeID string) string {
	h := sha256.New()
	h.Write(ciphertext)
	h.Write(iv)
	h.Write([]byte(nodeID))
	return hex.EncodeToString(h.Sum(nil))
}

// VerifyChain verifies the integrity of the entire chain
func (c *ChainKeyDerivation) VerifyChain() (*ChainVerificationResult, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := &ChainVerificationResult{
		Valid:          true,
		CheckedNodes:   len(c.chainNodes),
		InvalidNodes:   make([]string, 0),
		VerifiedAt:     time.Now().UnixNano(),
	}

	// Verify each node
	for i := 0; i < c.currentIndex; i++ {
		node := c.getNodeByIndex(i)
		if node == nil {
			result.Valid = false
			result.InvalidNodes = append(result.InvalidNodes, fmt.Sprintf("missing_node_%d", i))
			continue
		}

		// Verify key hash
		keyHash := sha256.Sum256(node.DerivedKey)
		if hex.EncodeToString(keyHash[:]) != node.KeyHash {
			result.Valid = false
			result.InvalidNodes = append(result.InvalidNodes, node.ID)
		}

		// Verify integrity hash
		expectedIntegrity := c.calculateIntegrityHash(node.ID, node.Index, keyHash[:])
		if expectedIntegrity != node.IntegrityHash {
			result.Valid = false
			result.InvalidNodes = append(result.InvalidNodes, node.ID)
		}

		// Verify chain continuity (next key hint)
		if i < c.currentIndex-1 {
			nextNode := c.getNodeByIndex(i + 1)
			if nextNode != nil {
				expectedHint := c.calculateNextKeyHint(node.DerivedKey, i+1)
				if expectedHint != node.NextKeyHint {
					result.Valid = false
					result.InvalidNodes = append(result.InvalidNodes, node.ID)
				}
			}
		}
	}

	return result, nil
}

// ChainVerificationResult represents the result of chain verification
type ChainVerificationResult struct {
	Valid        bool     `json:"valid"`
	CheckedNodes int      `json:"checked_nodes"`
	InvalidNodes []string `json:"invalid_nodes"`
	VerifiedAt   int64    `json:"verified_at"`
}

// GetChainInfo returns information about the chain (without exposing keys)
func (c *ChainKeyDerivation) GetChainInfo() *ChainInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	nodes := make([]ChainNodeInfo, 0, len(c.chainNodes))
	for _, node := range c.chainNodes {
		nodes = append(nodes, ChainNodeInfo{
			ID:            node.ID,
			Index:         node.Index,
			KeyHash:       node.KeyHash,
			IntegrityHash: node.IntegrityHash,
			CreatedAt:     node.CreatedAt,
		})
	}

	return &ChainInfo{
		TotalNodes:   len(c.chainNodes),
		CurrentIndex: c.currentIndex - 1,
		Nodes:        nodes,
	}
}

// ChainInfo represents chain information (safe to expose)
type ChainInfo struct {
	TotalNodes   int             `json:"total_nodes"`
	CurrentIndex int             `json:"current_index"`
	Nodes        []ChainNodeInfo `json:"nodes"`
}

// ChainNodeInfo represents node information (safe to expose)
type ChainNodeInfo struct {
	ID            string `json:"id"`
	Index         int    `json:"index"`
	KeyHash       string `json:"key_hash"`
	IntegrityHash string `json:"integrity_hash"`
	CreatedAt     int64  `json:"created_at"`
}
