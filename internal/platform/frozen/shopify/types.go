// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package shopify

// This file contains raw Shopify GraphQL response structs.
// They map exactly to Shopify's schema and are used as intermediate types
// before translation into UCP types in mapper.go.

// ─── Product / Catalog ────────────────────────────────────────────────────────

type shopifyProductSearchData struct {
	Products struct {
		Edges []struct {
			Cursor string         `json:"cursor"`
			Node   shopifyProduct `json:"node"`
		} `json:"edges"`
		PageInfo struct {
			HasNextPage bool   `json:"hasNextPage"`
			EndCursor   string `json:"endCursor"`
		} `json:"pageInfo"`
	} `json:"products"`
}

type shopifyProductByIDData struct {
	Product *shopifyProduct `json:"product"`
}

type shopifyProduct struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	Handle         string `json:"handle"`
	OnlineStoreURL string `json:"onlineStoreUrl"`
	PriceRange     struct {
		MinVariantPrice shopifyMoney `json:"minVariantPrice"`
	} `json:"priceRange"`
	FeaturedImage *struct {
		URL string `json:"url"`
	} `json:"featuredImage"`
	AvailableForSale bool `json:"availableForSale"`
	Variants         struct {
		Edges []struct {
			Node shopifyVariant `json:"node"`
		} `json:"edges"`
	} `json:"variants"`
}

type shopifyVariant struct {
	ID               string       `json:"id"`
	Title            string       `json:"title"`
	AvailableForSale bool         `json:"availableForSale"`
	Price            shopifyMoney `json:"price"`
	PriceV2          shopifyMoney `json:"priceV2"`
	SelectedOptions  []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"selectedOptions"`
}

type shopifyMoney struct {
	Amount       string `json:"amount"` // Shopify returns amounts as decimal strings
	CurrencyCode string `json:"currencyCode"`
}

// ─── Cart ─────────────────────────────────────────────────────────────────────

type shopifyCartCreateData struct {
	CartCreate struct {
		Cart       *shopifyCart       `json:"cart"`
		UserErrors []shopifyUserError `json:"userErrors"`
	} `json:"cartCreate"`
}

type shopifyCartGetData struct {
	Cart *shopifyCart `json:"cart"`
}

type shopifyCartLinesAddData struct {
	CartLinesAdd struct {
		Cart       *shopifyCart       `json:"cart"`
		UserErrors []shopifyUserError `json:"userErrors"`
	} `json:"cartLinesAdd"`
}

type shopifyCartLinesUpdateData struct {
	CartLinesUpdate struct {
		Cart       *shopifyCart       `json:"cart"`
		UserErrors []shopifyUserError `json:"userErrors"`
	} `json:"cartLinesUpdate"`
}

type shopifyCart struct {
	ID          string `json:"id"`
	CheckoutURL string `json:"checkoutUrl"`
	Lines       struct {
		Edges []struct {
			Node shopifyCartLine `json:"node"`
		} `json:"edges"`
	} `json:"lines"`
	Cost struct {
		SubtotalAmount shopifyMoney  `json:"subtotalAmount"`
		TotalTaxAmount *shopifyMoney `json:"totalTaxAmount"`
		TotalAmount    shopifyMoney  `json:"totalAmount"`
	} `json:"cost"`
	BuyerIdentity struct {
		Email string `json:"email"`
		Phone string `json:"phone"`
	} `json:"buyerIdentity"`

	// DiscountCodes holds codes that have been applied to this cart.
	DiscountCodes []struct {
		Code       string `json:"code"`
		Applicable bool   `json:"applicable"`
	} `json:"discountCodes"`
}

type shopifyCartLine struct {
	ID          string `json:"id"`
	Quantity    int    `json:"quantity"`
	Merchandise struct {
		ID      string       `json:"id"`
		Title   string       `json:"title"`
		Price   shopifyMoney `json:"price"`
		Product struct {
			ID            string `json:"id"`
			Title         string `json:"title"`
			FeaturedImage *struct {
				URL string `json:"url"`
			} `json:"featuredImage"`
		} `json:"product"`
	} `json:"merchandise"`
	Cost struct {
		SubtotalAmount shopifyMoney `json:"subtotalAmount"`
		TotalAmount    shopifyMoney `json:"totalAmount"`
	} `json:"cost"`
}

// ─── Checkout ─────────────────────────────────────────────────────────────────

type shopifyCheckoutCreateData struct {
	CheckoutCreate struct {
		Checkout   *shopifyCheckout       `json:"checkout"`
		UserErrors []shopifyCheckoutError `json:"checkoutUserErrors"`
	} `json:"checkoutCreate"`
}

type shopifyCheckoutGetData struct {
	Node *shopifyCheckout `json:"node"`
}

type shopifyCheckoutEmailUpdateData struct {
	CheckoutEmailUpdateV2 struct {
		Checkout   *shopifyCheckout       `json:"checkout"`
		UserErrors []shopifyCheckoutError `json:"checkoutUserErrors"`
	} `json:"checkoutEmailUpdateV2"`
}

type shopifyCheckoutShippingAddressUpdateData struct {
	CheckoutShippingAddressUpdateV2 struct {
		Checkout   *shopifyCheckout       `json:"checkout"`
		UserErrors []shopifyCheckoutError `json:"checkoutUserErrors"`
	} `json:"checkoutShippingAddressUpdateV2"`
}

type shopifyCheckoutShippingLineUpdateData struct {
	CheckoutShippingLineUpdate struct {
		Checkout   *shopifyCheckout       `json:"checkout"`
		UserErrors []shopifyCheckoutError `json:"checkoutUserErrors"`
	} `json:"checkoutShippingLineUpdate"`
}

type shopifyCheckoutCompleteData struct {
	CheckoutCompleteWithTokenizedPaymentV3 struct {
		Checkout   *shopifyCheckout       `json:"checkout"`
		UserErrors []shopifyCheckoutError `json:"checkoutUserErrors"`
		Payment    *shopifyPaymentResult  `json:"payment"`
	} `json:"checkoutCompleteWithTokenizedPaymentV3"`
}

type shopifyCheckout struct {
	ID          string  `json:"id"`
	WebURL      string  `json:"webUrl"`
	CompletedAt *string `json:"completedAt"`
	Email       string  `json:"email"`

	TotalPriceV2     shopifyMoney  `json:"totalPriceV2"`
	SubtotalPriceV2  shopifyMoney  `json:"subtotalPriceV2"`
	TotalTaxV2       *shopifyMoney `json:"totalTaxV2"`
	CurrencyCode     string        `json:"currencyCode"`
	RequiresShipping bool          `json:"requiresShipping"`

	ShippingLine *struct {
		Handle  string       `json:"handle"`
		Title   string       `json:"title"`
		PriceV2 shopifyMoney `json:"priceV2"`
	} `json:"shippingLine"`

	AvailableShippingRates *struct {
		Ready         bool `json:"ready"`
		ShippingRates []struct {
			Handle  string       `json:"handle"`
			Title   string       `json:"title"`
			PriceV2 shopifyMoney `json:"priceV2"`
		} `json:"shippingRates"`
	} `json:"availableShippingRates"`

	LineItems struct {
		Edges []struct {
			Node shopifyCheckoutLineItem `json:"node"`
		} `json:"edges"`
	} `json:"lineItems"`

	ShippingAddress *shopifyMailingAddress `json:"shippingAddress"`

	Order *struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		StatusURL string `json:"statusUrl"`
	} `json:"order"`

	// DiscountApplications holds applied and automatic discounts on the checkout.
	DiscountApplications struct {
		Edges []struct {
			Node shopifyDiscountApplication `json:"node"`
		} `json:"edges"`
	} `json:"discountApplications"`
}

// shopifyDiscountApplication is one applied or automatic discount on a checkout.
type shopifyDiscountApplication struct {
	// Code is the discount code; empty for automatic discounts.
	Code string `json:"code"`
	// Title is the human-readable discount name.
	Title string `json:"title"`
	// Value is a polymorphic amount — either a fixed money amount or a percentage.
	// We parse the flat amount string here; see moneyOrPercentageAmount helper.
	Value string `json:"value"` // decimal string from MoneyV2 or percentage
	// AllocationMethod: "EACH" | "ACROSS" | "LAST"
	AllocationMethod string `json:"allocationMethod"`
	// TargetType: "LINE_ITEM" | "SHIPPING_LINE"
	TargetType string `json:"targetType"`
	// Applicable is true when the code is currently valid.
	Applicable bool `json:"applicable"`
}

type shopifyCheckoutLineItem struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Quantity int    `json:"quantity"`
	Variant  *struct {
		ID      string       `json:"id"`
		Title   string       `json:"title"`
		PriceV2 shopifyMoney `json:"priceV2"`
		Product struct {
			ID            string `json:"id"`
			Title         string `json:"title"`
			FeaturedImage *struct {
				URL string `json:"url"`
			} `json:"featuredImage"`
		} `json:"product"`
	} `json:"variant"`
	DiscountAllocations []struct {
		AllocatedAmount  shopifyMoney `json:"allocatedAmount"`
		ApplicationIndex int          `json:"applicationIndex"`
	} `json:"discountAllocations"`
}

type shopifyMailingAddress struct {
	FirstName   string `json:"firstName"`
	LastName    string `json:"lastName"`
	Address1    string `json:"address1"`
	Address2    string `json:"address2"`
	City        string `json:"city"`
	Province    string `json:"province"`
	Country     string `json:"country"`
	CountryCode string `json:"countryCodeV2"` // ISO 3166-1 alpha-2 e.g. "AU"
	Zip         string `json:"zip"`
	Phone       string `json:"phone"`
}

type shopifyPaymentResult struct {
	ID           string `json:"id"`
	Ready        bool   `json:"ready"`
	ErrorMessage string `json:"errorMessage"`
	Transaction  *struct {
		Status   string `json:"status"`
		StatusV2 string `json:"statusV2"`
	} `json:"transaction"`
}

// ─── Shared ───────────────────────────────────────────────────────────────────

type shopifyUserError struct {
	Field   []string `json:"field"`
	Message string   `json:"message"`
	Code    string   `json:"code"`
}

type shopifyCheckoutError struct {
	Code    string   `json:"code"`
	Field   []string `json:"field"`
	Message string   `json:"message"`
}

// ─── Discount mutation responses ──────────────────────────────────────────────

type shopifyCheckoutDiscountCodeApplyData struct {
	CheckoutDiscountCodeApplyV2 struct {
		Checkout   *shopifyCheckout       `json:"checkout"`
		UserErrors []shopifyCheckoutError `json:"checkoutUserErrors"`
	} `json:"checkoutDiscountCodeApplyV2"`
}

type shopifyCheckoutDiscountCodeRemoveData struct {
	CheckoutDiscountCodeRemove struct {
		Checkout   *shopifyCheckout       `json:"checkout"`
		UserErrors []shopifyCheckoutError `json:"checkoutUserErrors"`
	} `json:"checkoutDiscountCodeRemove"`
}

// ─── Admin REST ───────────────────────────────────────────────────────────────

type shopifyAdminOrderResponse struct {
	Order shopifyAdminOrder `json:"order"`
}

type shopifyAdminOrder struct {
	ID                int64                     `json:"id"`
	Name              string                    `json:"name"`
	Email             string                    `json:"email"`
	FinancialStatus   string                    `json:"financial_status"`
	FulfillmentStatus string                    `json:"fulfillment_status"`
	CreatedAt         string                    `json:"created_at"`
	UpdatedAt         string                    `json:"updated_at"`
	TotalPrice        string                    `json:"total_price"`
	Currency          string                    `json:"currency"`
	LineItems         []shopifyAdminLineItem    `json:"line_items"`
	Fulfillments      []shopifyAdminFulfillment `json:"fulfillments"`
}

type shopifyAdminLineItem struct {
	ID                int64  `json:"id"`
	VariantID         int64  `json:"variant_id"`
	ProductID         int64  `json:"product_id"`
	Title             string `json:"title"`
	Quantity          int    `json:"quantity"`
	Price             string `json:"price"`
	FulfillmentStatus string `json:"fulfillment_status"`
}

type shopifyAdminFulfillment struct {
	ID              int64  `json:"id"`
	Status          string `json:"status"`
	TrackingCompany string `json:"tracking_company"`
	TrackingNumber  string `json:"tracking_number"`
	TrackingURL     string `json:"tracking_url"`
}
