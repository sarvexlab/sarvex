// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// This file holds the cart endpoints' wire contract: request bodies for the
// dev.ucp.shopping.cart capability (create_cart / update_cart). Orchestration
// lives in serving.CartService; handler.go only binds and responds.

// CartCreateRequest is the create_cart request body (v20260408.Cart's
// request-time fields: line_items is required; buyer, context and the discount
// extension's discounts are optional).
type CartCreateRequest struct {
	LineItems []base.LineItemCreateRequest `json:"line_items"`
	Buyer     *model.Buyer                 `json:"buyer,omitempty"`
	Context   *model.Context               `json:"context,omitempty"`
	Discounts *model.DiscountsObject       `json:"discounts,omitempty"`
}

// CartUpdateRequest is the update_cart request body (PUT, full replacement of
// the line items).
type CartUpdateRequest struct {
	LineItems []base.LineItemCreateRequest `json:"line_items"`
	Buyer     *model.Buyer                 `json:"buyer,omitempty"`
	Discounts *model.DiscountsObject       `json:"discounts,omitempty"`
}

// computeCartCreateBodyHash hashes a create body so reusing an Idempotency-Key
// with a different body is detected as a conflict.
func computeCartCreateBodyHash(req *CartCreateRequest) string {
	if req == nil {
		return ""
	}
	data, err := json.Marshal(req)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
