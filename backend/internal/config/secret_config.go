package config

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

func (m *ConfigManager) prepareGlobalConfig(ctx context.Context, incoming, current *GlobalConfig) (prepared *GlobalConfig, created []string, err error) {
	defer func() {
		if err == nil || len(created) == 0 {
			return
		}
		err = errors.Join(err, m.deleteSecretRefs(ctx, created))
	}()
	if incoming == nil {
		return nil, nil, fmt.Errorf("config cannot be nil")
	}
	if m.secretStore == nil {
		return nil, nil, fmt.Errorf("secret store is unavailable")
	}
	out := *incoming
	out.APIPool = make([]APIChannel, len(incoming.APIPool))
	oldByID := make(map[string]APIChannel)
	if current != nil {
		for _, channel := range current.APIPool {
			oldByID[channel.ID] = channel
		}
	}
	for i, input := range incoming.APIPool {
		if input.ID == "" {
			return nil, nil, fmt.Errorf("API channel ID cannot be empty")
		}
		old, hadOld := oldByID[input.ID]
		channel := input
		channel.APIKey = ""
		channel.DeleteSecret = false
		if input.APIKey != "" {
			ref := old.SecretRef
			if ref != "" {
				previous, err := m.readSecret(ctx, ref, nil)
				if err != nil {
					return nil, created, fmt.Errorf("read existing API channel secret: %w", err)
				}
				if previous == input.APIKey {
					metadata, err := m.secretStore.Metadata(ctx, ref)
					if err != nil {
						return nil, nil, fmt.Errorf("read API channel secret metadata: %w", err)
					}
					applySecretMetadata(&channel, metadata)
					out.APIPool[i] = channel
					continue
				}
			}
			ref = newSecretRef(input.ID)
			result, err := m.secretStore.Put(ctx, ref, input.APIKey)
			if err != nil {
				return nil, created, fmt.Errorf("store API channel secret: %w", err)
			}
			created = append(created, ref)
			channel.SecretRef = ref
			applySecretMetadata(&channel, result.Metadata)
			out.APIPool[i] = channel
			continue
		}

		if input.DeleteSecret {
			channel.SecretRef = ""
			channel.SecretStatus = SecretStatusMissing
			channel.MaskedValue = ""
			channel.SecretVersion = 0
			channel.SecretUpdatedAt = 0
			out.APIPool[i] = channel
			continue
		}

		if input.SecretRef == "" && hadOld && old.SecretRef != "" {
			channel.SecretRef = old.SecretRef
			channel.SecretStatus = old.SecretStatus
			channel.MaskedValue = old.MaskedValue
			channel.SecretVersion = old.SecretVersion
			channel.SecretUpdatedAt = old.SecretUpdatedAt
		} else if channel.SecretRef != "" {
			if !hadOld {
				return nil, created, fmt.Errorf("API channel secret reference cannot be supplied for a new channel")
			}
			metadata, err := m.readSecretMetadata(ctx, channel.SecretRef)
			if err != nil {
				return nil, created, fmt.Errorf("API channel secret reference is unavailable")
			}
			if hadOld && old.SecretRef != "" && old.SecretRef != channel.SecretRef {
				return nil, nil, fmt.Errorf("API channel secret reference cannot be changed without a new secret")
			}
			applySecretMetadata(&channel, metadata)
		} else {
			channel.SecretStatus = SecretStatusMissing
			channel.MaskedValue = ""
			channel.SecretVersion = 0
			channel.SecretUpdatedAt = 0
		}
		out.APIPool[i] = channel
	}
	return &out, created, nil
}

func applySecretMetadata(channel *APIChannel, metadata SecretMetadata) {
	channel.SecretRef = metadata.Ref
	channel.SecretStatus = metadata.Status
	channel.MaskedValue = metadata.MaskedValue
	channel.SecretVersion = metadata.Version
	channel.SecretUpdatedAt = metadata.UpdatedAt
}

func newSecretRef(channelID string) string {
	return "api-channel/" + channelID + "/" + uuid.NewString()
}

func uniqueSecretRefs(refs []string) []string {
	seen := make(map[string]struct{}, len(refs))
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func (m *ConfigManager) validateSecretReferences(ctx context.Context, config *GlobalConfig) error {
	if config == nil {
		return nil
	}
	for i := range config.APIPool {
		channel := &config.APIPool[i]
		channel.APIKey = ""
		channel.DeleteSecret = false
		if channel.SecretRef == "" {
			channel.SecretStatus = SecretStatusMissing
			channel.MaskedValue = ""
			channel.SecretVersion = 0
			channel.SecretUpdatedAt = 0
			continue
		}
		metadata, err := m.readSecretMetadata(ctx, channel.SecretRef)
		if err != nil {
			return fmt.Errorf("API channel %q credential is unavailable: %w", channel.ID, err)
		}
		applySecretMetadata(channel, metadata)
	}
	return nil
}

func (m *ConfigManager) readSecret(ctx context.Context, ref string, channel *APIChannel) (string, error) {
	if m == nil || m.secretStore == nil {
		return "", errors.New("secret store is unavailable")
	}
	value, err := m.secretStore.Get(ctx, ref)
	if err != nil {
		return "", err
	}
	metadata, err := m.secretStore.Metadata(ctx, ref)
	if err != nil {
		return "", err
	}
	if err := validateSecretMetadata(ref, value, metadata); err != nil {
		return "", err
	}
	if channel != nil {
		applySecretMetadata(channel, metadata)
	}
	return value, nil
}

func (m *ConfigManager) readSecretMetadata(ctx context.Context, ref string) (SecretMetadata, error) {
	if m == nil || m.secretStore == nil {
		return SecretMetadata{}, errors.New("secret store is unavailable")
	}
	value, err := m.secretStore.Get(ctx, ref)
	if err != nil {
		return SecretMetadata{}, err
	}
	metadata, err := m.secretStore.Metadata(ctx, ref)
	if err != nil {
		return SecretMetadata{}, err
	}
	if err := validateSecretMetadata(ref, value, metadata); err != nil {
		return SecretMetadata{}, err
	}
	return metadata, nil
}

func validateSecretMetadata(ref, value string, metadata SecretMetadata) error {
	if metadata.Ref != ref || metadata.Status != SecretStatusConfigured || metadata.Version < 1 {
		return errors.New("secret metadata is inconsistent")
	}
	if metadata.MaskedValue != maskSecret(value) {
		return errors.New("secret metadata does not match stored value")
	}
	return nil
}

func (m *ConfigManager) reconcileSecrets(ctx context.Context, config *GlobalConfig) ([]string, error) {
	metadata, err := m.secretStore.ListMetadata(ctx)
	if err != nil {
		return nil, fmt.Errorf("list API credentials: %w", err)
	}
	referenced := make(map[string]struct{})
	if config != nil {
		for _, channel := range config.APIPool {
			if channel.SecretRef != "" {
				referenced[channel.SecretRef] = struct{}{}
			}
		}
	}
	deleted := make([]string, 0)
	for _, item := range metadata {
		if _, ok := referenced[item.Ref]; ok {
			continue
		}
		removed, err := m.secretStore.Delete(ctx, item.Ref)
		if err != nil {
			return deleted, fmt.Errorf("delete unreferenced API credential: %w", err)
		}
		if removed {
			deleted = append(deleted, item.Ref)
		}
	}
	return deleted, nil
}

func (m *ConfigManager) deleteSecretRefs(ctx context.Context, refs []string) error {
	var failures []error
	for _, ref := range uniqueSecretRefs(refs) {
		if _, err := m.secretStore.Delete(ctx, ref); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
