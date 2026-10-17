// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package v20260408

// The spec permits freeform codes for several string fields (error/warning/info
// codes, event types, etc.) alongside a set of well-known values with
// standardized semantics. Per convention, unmarshaling never rejects an
// unrecognized value — callers that care use the type's Known method.

func knownSet[T comparable](values ...T) map[T]struct{} {
	m := make(map[T]struct{}, len(values))
	for _, v := range values {
		m[v] = struct{}{}
	}
	return m
}

// CheckoutStatus is the state of a checkout session, indicating the
// current phase and any required action.
type CheckoutStatus string

const (
	CheckoutStatusIncomplete         CheckoutStatus = "incomplete"
	CheckoutStatusRequiresEscalation CheckoutStatus = "requires_escalation"
	CheckoutStatusReadyForComplete   CheckoutStatus = "ready_for_complete"
	CheckoutStatusCompleteInProgress CheckoutStatus = "complete_in_progress"
	CheckoutStatusCompleted          CheckoutStatus = "completed"
	CheckoutStatusCanceled           CheckoutStatus = "canceled"
)

var knownCheckoutStatuses = knownSet(
	CheckoutStatusIncomplete,
	CheckoutStatusRequiresEscalation,
	CheckoutStatusReadyForComplete,
	CheckoutStatusCompleteInProgress,
	CheckoutStatusCompleted,
	CheckoutStatusCanceled,
)

// Known reports whether s is one of the standardized checkout statuses.
func (s CheckoutStatus) Known() bool { _, ok := knownCheckoutStatuses[s]; return ok }

// AdjustmentStatus is the state of a post-order Adjustment.
type AdjustmentStatus string

const (
	AdjustmentStatusPending   AdjustmentStatus = "pending"
	AdjustmentStatusCompleted AdjustmentStatus = "completed"
	AdjustmentStatusFailed    AdjustmentStatus = "failed"
)

var knownAdjustmentStatuses = knownSet(AdjustmentStatusPending, AdjustmentStatusCompleted, AdjustmentStatusFailed)

// Known reports whether s is one of the standardized adjustment statuses.
func (s AdjustmentStatus) Known() bool { _, ok := knownAdjustmentStatuses[s]; return ok }

// AdjustmentType is an open string describing the kind of post-order
// adjustment. Well-known values are provided as constants; merchants may
// use any value that makes sense for their business.
type AdjustmentType string

const (
	AdjustmentTypeRefund          AdjustmentType = "refund"
	AdjustmentTypeReturn          AdjustmentType = "return"
	AdjustmentTypeCredit          AdjustmentType = "credit"
	AdjustmentTypePriceAdjustment AdjustmentType = "price_adjustment"
	AdjustmentTypeDispute         AdjustmentType = "dispute"
	AdjustmentTypeCancellation    AdjustmentType = "cancellation"
)

var knownAdjustmentTypes = knownSet(
	AdjustmentTypeRefund,
	AdjustmentTypeReturn,
	AdjustmentTypeCredit,
	AdjustmentTypePriceAdjustment,
	AdjustmentTypeDispute,
	AdjustmentTypeCancellation,
)

// Known reports whether t is a well-known adjustment type.
func (t AdjustmentType) Known() bool { _, ok := knownAdjustmentTypes[t]; return ok }

// MessageSeverity reflects the resource state and recommended action for a
// MessageError.
type MessageSeverity string

const (
	// MessageSeverityRecoverable: the platform can resolve the issue by
	// modifying inputs and retrying via the API.
	MessageSeverityRecoverable MessageSeverity = "recoverable"
	// MessageSeverityRequiresBuyerInput: the merchant requires information
	// their API doesn't support collecting programmatically.
	MessageSeverityRequiresBuyerInput MessageSeverity = "requires_buyer_input"
	// MessageSeverityRequiresBuyerReview: the buyer must authorize before
	// order placement due to policy, regulatory, or entitlement rules.
	MessageSeverityRequiresBuyerReview MessageSeverity = "requires_buyer_review"
	// MessageSeverityUnrecoverable: no valid resource exists to act on.
	MessageSeverityUnrecoverable MessageSeverity = "unrecoverable"
)

var knownMessageSeverities = knownSet(
	MessageSeverityRecoverable,
	MessageSeverityRequiresBuyerInput,
	MessageSeverityRequiresBuyerReview,
	MessageSeverityUnrecoverable,
)

// Known reports whether s is one of the standardized message severities.
func (s MessageSeverity) Known() bool { _, ok := knownMessageSeverities[s]; return ok }

// RequiresEscalation reports whether this severity contributes to a
// checkout's status becoming "requires_escalation".
func (s MessageSeverity) RequiresEscalation() bool {
	return s == MessageSeverityRequiresBuyerInput || s == MessageSeverityRequiresBuyerReview
}

// MessageContentType is the format of a Message's content field.
type MessageContentType string

const (
	MessageContentTypePlain    MessageContentType = "plain"
	MessageContentTypeMarkdown MessageContentType = "markdown"
)

var knownMessageContentTypes = knownSet(MessageContentTypePlain, MessageContentTypeMarkdown)

// Known reports whether c is a standardized content type.
func (c MessageContentType) Known() bool { _, ok := knownMessageContentTypes[c]; return ok }

// WarningPresentation is the rendering contract for a MessageWarning.
type WarningPresentation string

const (
	// WarningPresentationNotice: platform MUST display, MAY dismiss. Default.
	WarningPresentationNotice WarningPresentation = "notice"
	// WarningPresentationDisclosure: platform MUST display in proximity to
	// the path-referenced component, MUST NOT hide or auto-dismiss.
	WarningPresentationDisclosure WarningPresentation = "disclosure"
)

var knownWarningPresentations = knownSet(WarningPresentationNotice, WarningPresentationDisclosure)

// Known reports whether p is a standardized presentation contract.
func (p WarningPresentation) Known() bool { _, ok := knownWarningPresentations[p]; return ok }

// ErrorCode identifies the type of error carried by a MessageError.
// Standard codes have standardized semantics; freeform codes are permitted.
type ErrorCode string

const (
	ErrorCodeNotFound             ErrorCode = "not_found"
	ErrorCodeOutOfStock           ErrorCode = "out_of_stock"
	ErrorCodeItemUnavailable      ErrorCode = "item_unavailable"
	ErrorCodeAddressUndeliverable ErrorCode = "address_undeliverable"
	ErrorCodePaymentFailed        ErrorCode = "payment_failed"
	ErrorCodeEligibilityInvalid   ErrorCode = "eligibility_invalid"
	ErrorCodeIdentityRequired     ErrorCode = "identity_required"
	ErrorCodeInsufficientScope    ErrorCode = "insufficient_scope"
)

var knownErrorCodes = knownSet(
	ErrorCodeNotFound,
	ErrorCodeOutOfStock,
	ErrorCodeItemUnavailable,
	ErrorCodeAddressUndeliverable,
	ErrorCodePaymentFailed,
	ErrorCodeEligibilityInvalid,
	ErrorCodeIdentityRequired,
	ErrorCodeInsufficientScope,
)

// Known reports whether c is a standardized error code.
func (c ErrorCode) Known() bool { _, ok := knownErrorCodes[c]; return ok }

// WarningCode identifies the type of warning carried by a MessageWarning.
// Standard codes have standardized semantics; freeform codes are permitted.
type WarningCode string

const (
	WarningCodeFinalSale          WarningCode = "final_sale"
	WarningCodeProp65             WarningCode = "prop65"
	WarningCodeFulfillmentChanged WarningCode = "fulfillment_changed"
	WarningCodeAgeRestricted      WarningCode = "age_restricted"
)

var knownWarningCodes = knownSet(
	WarningCodeFinalSale,
	WarningCodeProp65,
	WarningCodeFulfillmentChanged,
	WarningCodeAgeRestricted,
)

// Known reports whether c is a standardized warning code.
func (c WarningCode) Known() bool { _, ok := knownWarningCodes[c]; return ok }

// InfoCode identifies the type of informational message carried by a
// MessageInfo. Standard codes have standardized semantics; freeform codes
// are permitted.
type InfoCode string

const (
	InfoCodeIdentityOptional InfoCode = "identity_optional"
	InfoCodeSignal           InfoCode = "signal"
	InfoCodeFreeShipping     InfoCode = "free_shipping"
	InfoCodeNotFound         InfoCode = "not_found"
)

var knownInfoCodes = knownSet(
	InfoCodeIdentityOptional,
	InfoCodeSignal,
	InfoCodeFreeShipping,
	InfoCodeNotFound,
)

// Known reports whether c is a standardized info code.
func (c InfoCode) Known() bool { _, ok := knownInfoCodes[c]; return ok }

// FulfillmentMethodType is the delivery mechanism for a FulfillmentMethod
// or FulfillmentAvailableMethod.
type FulfillmentMethodType string

const (
	FulfillmentMethodTypeShipping FulfillmentMethodType = "shipping"
	FulfillmentMethodTypePickup   FulfillmentMethodType = "pickup"
)

var knownFulfillmentMethodTypes = knownSet(FulfillmentMethodTypeShipping, FulfillmentMethodTypePickup)

// Known reports whether t is a standardized fulfillment method type.
func (t FulfillmentMethodType) Known() bool { _, ok := knownFulfillmentMethodTypes[t]; return ok }

// ExpectationMethodType is the delivery mechanism for an Expectation. It
// additionally allows "digital", which does not apply to FulfillmentMethod.
type ExpectationMethodType string

const (
	ExpectationMethodTypeShipping ExpectationMethodType = "shipping"
	ExpectationMethodTypePickup   ExpectationMethodType = "pickup"
	ExpectationMethodTypeDigital  ExpectationMethodType = "digital"
)

var knownExpectationMethodTypes = knownSet(
	ExpectationMethodTypeShipping,
	ExpectationMethodTypePickup,
	ExpectationMethodTypeDigital,
)

// Known reports whether t is a standardized expectation method type.
func (t ExpectationMethodType) Known() bool { _, ok := knownExpectationMethodTypes[t]; return ok }

// FulfillmentEventType is an open string describing what happened in a
// FulfillmentEvent. Well-known values are provided as constants.
type FulfillmentEventType string

const (
	FulfillmentEventTypeProcessing       FulfillmentEventType = "processing"
	FulfillmentEventTypeShipped          FulfillmentEventType = "shipped"
	FulfillmentEventTypeInTransit        FulfillmentEventType = "in_transit"
	FulfillmentEventTypeDelivered        FulfillmentEventType = "delivered"
	FulfillmentEventTypeFailedAttempt    FulfillmentEventType = "failed_attempt"
	FulfillmentEventTypeCanceled         FulfillmentEventType = "canceled"
	FulfillmentEventTypeUndeliverable    FulfillmentEventType = "undeliverable"
	FulfillmentEventTypeReturnedToSender FulfillmentEventType = "returned_to_sender"
)

var knownFulfillmentEventTypes = knownSet(
	FulfillmentEventTypeProcessing,
	FulfillmentEventTypeShipped,
	FulfillmentEventTypeInTransit,
	FulfillmentEventTypeDelivered,
	FulfillmentEventTypeFailedAttempt,
	FulfillmentEventTypeCanceled,
	FulfillmentEventTypeUndeliverable,
	FulfillmentEventTypeReturnedToSender,
)

// Known reports whether t is a well-known fulfillment event type.
func (t FulfillmentEventType) Known() bool { _, ok := knownFulfillmentEventTypes[t]; return ok }

// CardNumberType is the kind of card number carried by a CardCredential.
type CardNumberType string

const (
	CardNumberTypeFPAN         CardNumberType = "fpan"
	CardNumberTypeNetworkToken CardNumberType = "network_token"
	CardNumberTypeDPAN         CardNumberType = "dpan"
)

var knownCardNumberTypes = knownSet(CardNumberTypeFPAN, CardNumberTypeNetworkToken, CardNumberTypeDPAN)

// Known reports whether t is a standardized card number type.
func (t CardNumberType) Known() bool { _, ok := knownCardNumberTypes[t]; return ok }

// VariantAvailabilityStatus qualifies Variant.Availability.Available with
// fulfillment state. Open string; well-known values provided as constants.
type VariantAvailabilityStatus string

const (
	VariantAvailabilityStatusInStock      VariantAvailabilityStatus = "in_stock"
	VariantAvailabilityStatusBackorder    VariantAvailabilityStatus = "backorder"
	VariantAvailabilityStatusPreorder     VariantAvailabilityStatus = "preorder"
	VariantAvailabilityStatusOutOfStock   VariantAvailabilityStatus = "out_of_stock"
	VariantAvailabilityStatusDiscontinued VariantAvailabilityStatus = "discontinued"
)

var knownVariantAvailabilityStatuses = knownSet(
	VariantAvailabilityStatusInStock,
	VariantAvailabilityStatusBackorder,
	VariantAvailabilityStatusPreorder,
	VariantAvailabilityStatusOutOfStock,
	VariantAvailabilityStatusDiscontinued,
)

// Known reports whether s is a well-known variant availability status.
func (s VariantAvailabilityStatus) Known() bool {
	_, ok := knownVariantAvailabilityStatuses[s]
	return ok
}

// LinkType categorizes a Link. Open string; well-known values provided as
// constants.
type LinkType string

const (
	LinkTypePrivacyPolicy  LinkType = "privacy_policy"
	LinkTypeTermsOfService LinkType = "terms_of_service"
	LinkTypeRefundPolicy   LinkType = "refund_policy"
	LinkTypeShippingPolicy LinkType = "shipping_policy"
	LinkTypeFAQ            LinkType = "faq"
)

var knownLinkTypes = knownSet(
	LinkTypePrivacyPolicy,
	LinkTypeTermsOfService,
	LinkTypeRefundPolicy,
	LinkTypeShippingPolicy,
	LinkTypeFAQ,
)

// Known reports whether t is a well-known link type.
func (t LinkType) Known() bool { _, ok := knownLinkTypes[t]; return ok }

// MediaType categorizes a Media item. Open string; well-known values
// provided as constants.
type MediaType string

const (
	MediaTypeImage   MediaType = "image"
	MediaTypeVideo   MediaType = "video"
	MediaTypeModel3D MediaType = "model_3d"
)

var knownMediaTypes = knownSet(MediaTypeImage, MediaTypeVideo, MediaTypeModel3D)

// Known reports whether t is a well-known media type.
func (t MediaType) Known() bool { _, ok := knownMediaTypes[t]; return ok }

// BarcodeType categorizes a Variant barcode. Open string; well-known
// values provided as constants.
type BarcodeType string

const (
	BarcodeTypeUPC  BarcodeType = "UPC"
	BarcodeTypeEAN  BarcodeType = "EAN"
	BarcodeTypeISBN BarcodeType = "ISBN"
	BarcodeTypeGTIN BarcodeType = "GTIN"
	BarcodeTypeJAN  BarcodeType = "JAN"
)

var knownBarcodeTypes = knownSet(BarcodeTypeUPC, BarcodeTypeEAN, BarcodeTypeISBN, BarcodeTypeGTIN, BarcodeTypeJAN)

// Known reports whether t is a well-known barcode type.
func (t BarcodeType) Known() bool { _, ok := knownBarcodeTypes[t]; return ok }

// TotalType categorizes a Total entry. Open string; well-known values
// provided as constants. Totals.Validate relies on the "subtotal" and
// "total" values.
type TotalType string

const (
	TotalTypeSubtotal      TotalType = "subtotal"
	TotalTypeItemsDiscount TotalType = "items_discount"
	TotalTypeDiscount      TotalType = "discount"
	TotalTypeFulfillment   TotalType = "fulfillment"
	TotalTypeTax           TotalType = "tax"
	TotalTypeFee           TotalType = "fee"
	TotalTypeTotal         TotalType = "total"
)

var knownTotalTypes = knownSet(
	TotalTypeSubtotal,
	TotalTypeItemsDiscount,
	TotalTypeDiscount,
	TotalTypeFulfillment,
	TotalTypeTax,
	TotalTypeFee,
	TotalTypeTotal,
)

// Known reports whether t is a well-known total type.
func (t TotalType) Known() bool { _, ok := knownTotalTypes[t]; return ok }

// InputCorrelationMatch describes how a lookup request identifier resolved
// to a variant. Open string; well-known values provided as constants.
type InputCorrelationMatch string

const (
	// InputCorrelationMatchExact: input directly identifies this variant
	// (e.g., variant ID, SKU).
	InputCorrelationMatchExact InputCorrelationMatch = "exact"
	// InputCorrelationMatchFeatured: server selected this variant as
	// representative (e.g., product ID resolved to best match).
	InputCorrelationMatchFeatured InputCorrelationMatch = "featured"
)

var knownInputCorrelationMatches = knownSet(InputCorrelationMatchExact, InputCorrelationMatchFeatured)

// Known reports whether m is a well-known match strategy.
func (m InputCorrelationMatch) Known() bool { _, ok := knownInputCorrelationMatches[m]; return ok }

// AllocationMethod is how a discount amount is spread across a Cart or
// Checkout's line items.
type AllocationMethod string

const (
	// AllocationMethodEach: applied independently per item.
	AllocationMethodEach AllocationMethod = "each"
	// AllocationMethodAcross: split proportionally by value.
	AllocationMethodAcross AllocationMethod = "across"
)

var knownAllocationMethods = knownSet(AllocationMethodEach, AllocationMethodAcross)

// Known reports whether m is a standardized allocation method.
func (m AllocationMethod) Known() bool { _, ok := knownAllocationMethods[m]; return ok }

// OrderLineItemStatus is the derived fulfillment status of an
// OrderLineItem.
type OrderLineItemStatus string

const (
	OrderLineItemStatusProcessing OrderLineItemStatus = "processing"
	OrderLineItemStatusPartial    OrderLineItemStatus = "partial"
	OrderLineItemStatusFulfilled  OrderLineItemStatus = "fulfilled"
	OrderLineItemStatusRemoved    OrderLineItemStatus = "removed"
)

var knownOrderLineItemStatuses = knownSet(
	OrderLineItemStatusProcessing,
	OrderLineItemStatusPartial,
	OrderLineItemStatusFulfilled,
	OrderLineItemStatusRemoved,
)

// Known reports whether s is one of the standardized order line item statuses.
func (s OrderLineItemStatus) Known() bool { _, ok := knownOrderLineItemStatuses[s]; return ok }

// UCPStatus is the application-level status of a UCP operation, carried in
// UCP metadata.
type UCPStatus string

const (
	UCPStatusSuccess UCPStatus = "success"
	UCPStatusError   UCPStatus = "error"
)

var knownUCPStatuses = knownSet(UCPStatusSuccess, UCPStatusError)

// Known reports whether s is a standardized UCP status.
func (s UCPStatus) Known() bool { _, ok := knownUCPStatuses[s]; return ok }

// ServiceTransport is the transport protocol for a Service binding.
type ServiceTransport string

const (
	ServiceTransportREST     ServiceTransport = "rest"
	ServiceTransportMCP      ServiceTransport = "mcp"
	ServiceTransportA2A      ServiceTransport = "a2a"
	ServiceTransportEmbedded ServiceTransport = "embedded"
)

var knownServiceTransports = knownSet(
	ServiceTransportREST,
	ServiceTransportMCP,
	ServiceTransportA2A,
	ServiceTransportEmbedded,
)

// Known reports whether t is a standardized service transport.
func (t ServiceTransport) Known() bool { _, ok := knownServiceTransports[t]; return ok }

// SigningKeyUse is the usage of a discovery profile's signing key.
type SigningKeyUse string

const (
	SigningKeyUseSig SigningKeyUse = "sig"
	SigningKeyUseEnc SigningKeyUse = "enc"
)

var knownSigningKeyUses = knownSet(SigningKeyUseSig, SigningKeyUseEnc)

// Known reports whether u is a standardized signing key use.
func (u SigningKeyUse) Known() bool { _, ok := knownSigningKeyUses[u]; return ok }
