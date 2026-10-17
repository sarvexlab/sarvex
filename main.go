// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/joho/godotenv"
	"github.com/sarvexlabs/sarvex.cp/cmd"
)

func main() {
	godotenv.Load()
	cmd.Execute()
}
