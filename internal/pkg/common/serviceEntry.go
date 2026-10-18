// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package common

type ServiceEntry interface {
	Init(tls *bool, port *int, env *string) error
}
