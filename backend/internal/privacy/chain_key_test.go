// Package privacy - Chain Key Derivation tests
package privacy

import (
	"context"
	"testing"
)

func TestChainKeyDerivation_Create(t *testing.T) {
	ckd, err := NewChainKeyDerivation("test-password")
	if err != nil {
		t.Fatalf("failed to create chain key derivation: %v", err)
	}

	// Should have one node after creation
	info := ckd.GetChainInfo()
	if info.TotalNodes != 1 {
		t.Errorf("expected 1 node, got %d", info.TotalNodes)
	}

	// Current node should exist
	currentNode := ckd.GetCurrentNode()
	if currentNode == nil {
		t.Error("expected current node to exist")
	}

	if currentNode.Index != 0 {
		t.Errorf("expected index 0, got %d", currentNode.Index)
	}

	if currentNode.KeyHash == "" {
		t.Error("expected non-empty key hash")
	}

	if currentNode.IntegrityHash == "" {
		t.Error("expected non-empty integrity hash")
	}
}

func TestChainKeyDerivation_EncryptDecrypt(t *testing.T) {
	ckd, err := NewChainKeyDerivation("test-password")
	if err != nil {
		t.Fatalf("failed to create chain key derivation: %v", err)
	}

	ctx := context.Background()
	plaintext := "This is a secret message for chain encryption"

	// Encrypt
	encrypted, err := ckd.Encrypt(ctx, plaintext)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	if encrypted.NodeID == "" {
		t.Error("expected non-empty node ID")
	}

	if encrypted.Ciphertext == "" {
		t.Error("expected non-empty ciphertext")
	}

	if encrypted.IV == "" {
		t.Error("expected non-empty IV")
	}

	if encrypted.IntegrityHash == "" {
		t.Error("expected non-empty integrity hash")
	}

	// Decrypt
	decrypted, err := ckd.Decrypt(ctx, encrypted)
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("expected %q, got %q", plaintext, decrypted)
	}
}

func TestChainKeyDerivation_KeyRotation(t *testing.T) {
	ckd, err := NewChainKeyDerivation("test-password")
	if err != nil {
		t.Fatalf("failed to create chain key derivation: %v", err)
	}

	ctx := context.Background()

	// Encrypt with first key
	plaintext := "Secret data"
	encrypted1, err := ckd.Encrypt(ctx, plaintext)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	// Rotate key
	newNode, err := ckd.RotateKey()
	if err != nil {
		t.Fatalf("key rotation failed: %v", err)
	}

	if newNode.Index != 1 {
		t.Errorf("expected new node index 1, got %d", newNode.Index)
	}

	// Should have 2 nodes now
	info := ckd.GetChainInfo()
	if info.TotalNodes != 2 {
		t.Errorf("expected 2 nodes, got %d", info.TotalNodes)
	}

	// Encrypt with new key
	encrypted2, err := ckd.Encrypt(ctx, plaintext)
	if err != nil {
		t.Fatalf("encryption with new key failed: %v", err)
	}

	// Node IDs should be different
	if encrypted1.NodeID == encrypted2.NodeID {
		t.Error("expected different node IDs after rotation")
	}

	// Both should decrypt correctly
	decrypted1, err := ckd.Decrypt(ctx, encrypted1)
	if err != nil {
		t.Fatalf("decryption of old data failed: %v", err)
	}
	if decrypted1 != plaintext {
		t.Errorf("expected %q, got %q", plaintext, decrypted1)
	}

	decrypted2, err := ckd.Decrypt(ctx, encrypted2)
	if err != nil {
		t.Fatalf("decryption of new data failed: %v", err)
	}
	if decrypted2 != plaintext {
		t.Errorf("expected %q, got %q", plaintext, decrypted2)
	}
}

func TestChainKeyDerivation_VerifyChain(t *testing.T) {
	ckd, err := NewChainKeyDerivation("test-password")
	if err != nil {
		t.Fatalf("failed to create chain key derivation: %v", err)
	}

	// Add more nodes
	for i := 0; i < 3; i++ {
		_, err := ckd.RotateKey()
		if err != nil {
			t.Fatalf("key rotation failed: %v", err)
		}
	}

	// Verify chain
	result, err := ckd.VerifyChain()
	if err != nil {
		t.Fatalf("chain verification failed: %v", err)
	}

	if !result.Valid {
		t.Error("expected chain to be valid")
	}

	if result.CheckedNodes != 4 {
		t.Errorf("expected 4 checked nodes, got %d", result.CheckedNodes)
	}

	if len(result.InvalidNodes) != 0 {
		t.Errorf("expected no invalid nodes, got %v", result.InvalidNodes)
	}
}

func TestChainKeyDerivation_IntegrityCheck(t *testing.T) {
	ckd, err := NewChainKeyDerivation("test-password")
	if err != nil {
		t.Fatalf("failed to create chain key derivation: %v", err)
	}

	ctx := context.Background()
	plaintext := "Test data"

	encrypted, err := ckd.Encrypt(ctx, plaintext)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	// Tamper with integrity hash
	encrypted.IntegrityHash = "tampered_hash"

	// Decryption should fail
	_, err = ckd.Decrypt(ctx, encrypted)
	if err == nil {
		t.Error("expected decryption to fail with tampered integrity hash")
	}
}

func TestChainKeyDerivation_InvalidNodeID(t *testing.T) {
	ckd, err := NewChainKeyDerivation("test-password")
	if err != nil {
		t.Fatalf("failed to create chain key derivation: %v", err)
	}

	ctx := context.Background()

	// Create encrypted data with invalid node ID
	encrypted := &ChainEncryptedData{
		NodeID:        "invalid_node_id",
		Ciphertext:    "some_ciphertext",
		IV:            "some_iv",
		IntegrityHash: "some_hash",
	}

	// Decryption should fail
	_, err = ckd.Decrypt(ctx, encrypted)
	if err == nil {
		t.Error("expected decryption to fail with invalid node ID")
	}
}

func TestChainKeyDerivation_GetNode(t *testing.T) {
	ckd, err := NewChainKeyDerivation("test-password")
	if err != nil {
		t.Fatalf("failed to create chain key derivation: %v", err)
	}

	currentNode := ckd.GetCurrentNode()
	if currentNode == nil {
		t.Fatal("expected current node to exist")
	}

	// Get node by ID
	node := ckd.GetNode(currentNode.ID)
	if node == nil {
		t.Error("expected to find node by ID")
	}

	if node.ID != currentNode.ID {
		t.Errorf("expected ID %s, got %s", currentNode.ID, node.ID)
	}

	// Get non-existent node
	node = ckd.GetNode("non_existent_id")
	if node != nil {
		t.Error("expected nil for non-existent node")
	}
}

func TestChainKeyDerivation_ChainInfo(t *testing.T) {
	ckd, err := NewChainKeyDerivation("test-password")
	if err != nil {
		t.Fatalf("failed to create chain key derivation: %v", err)
	}

	// Rotate a few times
	for i := 0; i < 3; i++ {
		ckd.RotateKey()
	}

	info := ckd.GetChainInfo()

	if info.TotalNodes != 4 {
		t.Errorf("expected 4 total nodes, got %d", info.TotalNodes)
	}

	if info.CurrentIndex != 3 {
		t.Errorf("expected current index 3, got %d", info.CurrentIndex)
	}

	if len(info.Nodes) != 4 {
		t.Errorf("expected 4 nodes in info, got %d", len(info.Nodes))
	}

	// Verify node info doesn't expose keys
	for _, nodeInfo := range info.Nodes {
		if nodeInfo.KeyHash == "" {
			t.Error("expected non-empty key hash in node info")
		}
		if nodeInfo.IntegrityHash == "" {
			t.Error("expected non-empty integrity hash in node info")
		}
	}
}

func TestChainKeyDerivation_MultipleRotations(t *testing.T) {
	ckd, err := NewChainKeyDerivation("test-password")
	if err != nil {
		t.Fatalf("failed to create chain key derivation: %v", err)
	}

	ctx := context.Background()
	plaintext := "Persistent secret"

	// Encrypt with initial key
	encrypted, err := ckd.Encrypt(ctx, plaintext)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	// Rotate multiple times
	for i := 0; i < 10; i++ {
		_, err := ckd.RotateKey()
		if err != nil {
			t.Fatalf("rotation %d failed: %v", i, err)
		}
	}

	// Should still be able to decrypt old data
	decrypted, err := ckd.Decrypt(ctx, encrypted)
	if err != nil {
		t.Fatalf("decryption after multiple rotations failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("expected %q, got %q", plaintext, decrypted)
	}

	// Verify chain integrity
	result, err := ckd.VerifyChain()
	if err != nil {
		t.Fatalf("chain verification failed: %v", err)
	}

	if !result.Valid {
		t.Error("expected chain to be valid after multiple rotations")
	}

	if result.CheckedNodes != 11 {
		t.Errorf("expected 11 checked nodes, got %d", result.CheckedNodes)
	}
}
