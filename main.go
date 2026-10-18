// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/joho/godotenv"
	"github.com/sarvexlab/sarvex/cmd"
)

func main() {
	godotenv.Load()
	cmd.Execute()
}
