// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package secretmanagement

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	vaultapi "github.com/hashicorp/vault/api"
)

// vaultService stores config maps in Vault's KV v2 engine. KV v2 stores an
// arbitrary JSON object natively, so Encrypt/Decrypt pass the config map
// straight through without an extra marshal/unmarshal step.
type vaultService struct {
	client    *vaultapi.Logical
	mountPath string
}

var _ SecretManagementService = (*vaultService)(nil)

// newVaultService builds a Vault API client pointed at cfg.VaultAddr.
func newVaultService(cfg Config) (SecretManagementService, error) {
	if cfg.VaultAddr == "" {
		return nil, fmt.Errorf("VAULT_ADDR is required for vault secret management mode")
	}

	vcfg := vaultapi.DefaultConfig()
	vcfg.Address = cfg.VaultAddr
	client, err := vaultapi.NewClient(vcfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create vault client: %w", err)
	}
	if cfg.VaultToken != "" {
		client.SetToken(cfg.VaultToken)
	}
	// Otherwise falls back to the Vault Agent Injector's rendered token —
	// see internal/pkg/jwkmanagement/vault.go's identical comment.

	mountPath := cfg.VaultMountPath
	if mountPath == "" {
		mountPath = "secret"
	}

	return &vaultService{client: client.Logical(), mountPath: mountPath}, nil
}

// kvPath builds the KV v2 data path for a given secret ID — every secret
// this package creates lives under <mount>/data/sarvex/<id>.
func (s *vaultService) kvPath(id string) string {
	return fmt.Sprintf("%s/data/sarvex/%s", s.mountPath, id)
}

// Encrypt writes config as a new KV v2 secret and returns a reference to
// the version Vault assigned it.
func (s *vaultService) Encrypt(ctx context.Context, config map[string]any) (*EncryptedConfig, error) {
	if config == nil {
		config = make(map[string]any)
	}

	path := s.kvPath(uuid.New().String())
	secret, err := s.client.WriteWithContext(ctx, path, map[string]interface{}{"data": config})
	if err != nil {
		return nil, fmt.Errorf("failed to write vault secret: %w", err)
	}

	version := "1"
	if secret != nil {
		if v, ok := secret.Data["version"]; ok {
			version = fmt.Sprintf("%v", v)
		}
	}

	return &EncryptedConfig{SecretName: path, Version: version}, nil
}

// Decrypt reads the referenced KV v2 secret version back into a config map.
func (s *vaultService) Decrypt(ctx context.Context, encrypted *EncryptedConfig) (map[string]any, error) {
	if encrypted == nil {
		return nil, fmt.Errorf("encrypted config cannot be nil")
	}

	secret, err := s.client.ReadWithDataWithContext(ctx, encrypted.SecretName, map[string][]string{
		"version": {encrypted.Version},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to read vault secret: %w", err)
	}
	if secret == nil || secret.Data == nil {
		return nil, fmt.Errorf("vault secret not found: %s", encrypted.SecretName)
	}

	data, ok := secret.Data["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("vault secret %s has an unexpected shape", encrypted.SecretName)
	}

	return data, nil
}
