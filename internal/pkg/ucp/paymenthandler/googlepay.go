// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package paymenthandler

type GooglePayMerchantInfo struct {
	MerchantName   string `json:"merchant_name" validate:"required"`
	MerchantID     string `json:"merchant_id" validate:"required"`
	MerchantOrigin string `json:"merchant_origin" validate:"required"`
}

type GooglePayBillingAddressParameters struct {
	Format              string `json:"format,omitempty"`
	PhoneNumberRequired bool   `json:"phone_number_required,omitempty"`
}

type GooglePayCardParameters struct {
	AllowedAuthMethods       []string                           `json:"allowed_auth_methods" validate:"required"`
	AllowedCardNetworks      []string                           `json:"allowed_card_networks" validate:"required"`
	BillingAddressRequired   bool                               `json:"billing_address_required,omitempty"`
	BillingAddressParameters *GooglePayBillingAddressParameters `json:"billing_address_parameters,omitempty"`
}

type GooglePayTokenizationParameters struct {
	Gateway           string `json:"gateway" validate:"required"`
	GatewayMerchantID string `json:"gatewayMerchantId" validate:"required"`
}

type GooglePayTokenizationSpecification struct {
	Type       string                          `json:"type" validate:"required"`
	Parameters GooglePayTokenizationParameters `json:"parameters" validate:"required"`
}

type GooglePayAllowedPaymentMethod struct {
	Type                      string                             `json:"type" validate:"required"`
	Parameters                GooglePayCardParameters            `json:"parameters" validate:"required"`
	TokenizationSpecification GooglePayTokenizationSpecification `json:"tokenization_specification" validate:"required"`
}

type GooglePayConfig struct {
	APIVersion            int                             `json:"api_version" validate:"required"`
	APIVersionMinor       int                             `json:"api_version_minor" validate:"required"`
	Environment           string                          `json:"environment" validate:"required"`
	MerchantInfo          GooglePayMerchantInfo           `json:"merchant_info" validate:"required"`
	AllowedPaymentMethods []GooglePayAllowedPaymentMethod `json:"allowed_payment_methods" validate:"required"`
}

func DummyGooglePayConfig() GooglePayConfig {
	return GooglePayConfig{
		APIVersion:      2,
		APIVersionMinor: 0,
		Environment:     "TEST",
		MerchantInfo: GooglePayMerchantInfo{
			MerchantName:   "Example Merchant",
			MerchantID:     "01234567890123456789",
			MerchantOrigin: "https://example.com",
		},
		AllowedPaymentMethods: []GooglePayAllowedPaymentMethod{
			{
				Type: "CARD",
				Parameters: GooglePayCardParameters{
					AllowedAuthMethods:     []string{"PAN_ONLY", "CRYPTOGRAM_3DS"},
					AllowedCardNetworks:    []string{"AMEX", "DISCOVER", "JCB", "MASTERCARD", "VISA"},
					BillingAddressRequired: true,
					BillingAddressParameters: &GooglePayBillingAddressParameters{
						Format:              "FULL",
						PhoneNumberRequired: true,
					},
				},
				TokenizationSpecification: GooglePayTokenizationSpecification{
					Type: "PAYMENT_GATEWAY",
					Parameters: GooglePayTokenizationParameters{
						Gateway:           "example",
						GatewayMerchantID: "exampleGatewayMerchantId",
					},
				},
			},
		},
	}
}
