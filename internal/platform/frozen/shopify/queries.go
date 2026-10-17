// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package shopify

// This file contains all Shopify Storefront API GraphQL query/mutation strings
// used by the connector. Keeping them here makes schema diffs easy to spot and
// version independently from the translation logic.
//
// All queries target the Storefront API 2025-01 schema unless noted.

const gqlProductSearch = `
query ProductSearch($query: String!, $first: Int!, $after: String) {
  products(query: $query, first: $first, after: $after) {
    edges {
      cursor
      node {
        id
        title
        description
        handle
        onlineStoreUrl
        priceRange {
          minVariantPrice {
            amount
            currencyCode
          }
        }
        featuredImage {
          url
        }
        availableForSale
        variants(first: 10) {
          edges {
            node {
              id
              title
              availableForSale
              price {
                amount
                currencyCode
              }
              selectedOptions {
                name
                value
              }
            }
          }
        }
      }
    }
    pageInfo {
      hasNextPage
      endCursor
    }
  }
}`

const gqlProductByID = `
query ProductByID($id: ID!) {
  product(id: $id) {
    id
    title
    description
    handle
    onlineStoreUrl
    priceRange {
      minVariantPrice {
        amount
        currencyCode
      }
    }
    featuredImage {
      url
    }
    availableForSale
    variants(first: 50) {
      edges {
        node {
          id
          title
          availableForSale
          price {
            amount
            currencyCode
          }
          selectedOptions {
            name
            value
          }
        }
      }
    }
  }
}`

const gqlCartCreate = `
mutation CartCreate($input: CartInput!) {
  cartCreate(input: $input) {
    cart {
      id
      checkoutUrl
      lines(first: 50) {
        edges {
          node {
            id
            quantity
            merchandise {
              ... on ProductVariant {
                id
                title
                price {
                  amount
                  currencyCode
                }
                product {
                  id
                  title
                  featuredImage { url }
                }
              }
            }
            cost {
              subtotalAmount { amount currencyCode }
              totalAmount { amount currencyCode }
            }
          }
        }
      }
      cost {
        subtotalAmount { amount currencyCode }
        totalTaxAmount { amount currencyCode }
        totalAmount { amount currencyCode }
      }
      buyerIdentity {
        email
        phone
      }
      discountCodes {
        code
        applicable
      }
    }
    userErrors {
      field
      message
      code
    }
  }
}`

const gqlCartGet = `
query CartGet($cartId: ID!) {
  cart(id: $cartId) {
    id
    checkoutUrl
    lines(first: 50) {
      edges {
        node {
          id
          quantity
          merchandise {
            ... on ProductVariant {
              id
              title
              price { amount currencyCode }
              product {
                id
                title
                featuredImage { url }
              }
            }
          }
          cost {
            subtotalAmount { amount currencyCode }
            totalAmount { amount currencyCode }
          }
        }
      }
    }
    cost {
      subtotalAmount { amount currencyCode }
      totalTaxAmount { amount currencyCode }
      totalAmount { amount currencyCode }
    }
    buyerIdentity {
      email
      phone
    }
    discountCodes {
      code
      applicable
    }
  }
}`

const gqlCartLinesAdd = `
mutation CartLinesAdd($cartId: ID!, $lines: [CartLineInput!]!) {
  cartLinesAdd(cartId: $cartId, lines: $lines) {
    cart {
      id
      checkoutUrl
      lines(first: 50) {
        edges {
          node {
            id
            quantity
            merchandise {
              ... on ProductVariant {
                id
                title
                price { amount currencyCode }
                product {
                  id
                  title
                  featuredImage { url }
                }
              }
            }
            cost {
              subtotalAmount { amount currencyCode }
              totalAmount { amount currencyCode }
            }
          }
        }
      }
      cost {
        subtotalAmount { amount currencyCode }
        totalTaxAmount { amount currencyCode }
        totalAmount { amount currencyCode }
      }
    }
    userErrors { field message code }
  }
}`

const gqlCartLinesUpdate = `
mutation CartLinesUpdate($cartId: ID!, $lines: [CartLineUpdateInput!]!) {
  cartLinesUpdate(cartId: $cartId, lines: $lines) {
    cart {
      id
      checkoutUrl
      lines(first: 50) {
        edges {
          node {
            id
            quantity
            merchandise {
              ... on ProductVariant {
                id
                title
                price { amount currencyCode }
                product {
                  id
                  title
                  featuredImage { url }
                }
              }
            }
            cost {
              subtotalAmount { amount currencyCode }
              totalAmount { amount currencyCode }
            }
          }
        }
      }
      cost {
        subtotalAmount { amount currencyCode }
        totalTaxAmount { amount currencyCode }
        totalAmount { amount currencyCode }
      }
    }
    userErrors { field message code }
  }
}`

// Checkout mutations — these use the Checkout object (older API) which supports
// payment tokenisation. The newer Cart API redirects to Shopify's hosted checkout.

const gqlCheckoutCreate = `
mutation CheckoutCreate($input: CheckoutCreateInput!) {
  checkoutCreate(input: $input) {
    checkout {
      id
      webUrl
      completedAt
      email
      totalPriceV2 { amount currencyCode }
      subtotalPriceV2 { amount currencyCode }
      totalTaxV2 { amount currencyCode }
      shippingLine {
        handle
        title
        priceV2 { amount currencyCode }
      }
      availableShippingRates {
        ready
        shippingRates {
          handle
          title
          priceV2 { amount currencyCode }
        }
      }
      lineItems(first: 50) {
        edges {
          node {
            id
            title
            quantity
            variant {
              id
              title
              priceV2 { amount currencyCode }
              product {
                id
                title
                featuredImage { url }
              }
            }
            discountAllocations {
              allocatedAmount { amount currencyCode }
              applicationIndex
            }
          }
        }
      }
      shippingAddress {
        firstName lastName
        address1 address2
        city province country countryCodeV2 zip
        phone
      }
      discountApplications(first: 10) {
        edges {
          node {
            ... on DiscountCodeApplication {
              code
              applicable
              allocationMethod
              targetType
              value {
                ... on MoneyV2 { amount currencyCode }
                ... on PricingPercentageValue { percentage }
              }
            }
            ... on AutomaticDiscountApplication {
              title
              allocationMethod
              targetType
              value {
                ... on MoneyV2 { amount currencyCode }
                ... on PricingPercentageValue { percentage }
              }
            }
          }
        }
      }
      currencyCode
      requiresShipping
    }
    checkoutUserErrors {
      code field message
    }
  }
}`

const gqlCheckoutGet = `
query CheckoutGet($checkoutId: ID!) {
  node(id: $checkoutId) {
    ... on Checkout {
      id
      webUrl
      completedAt
      email
      totalPriceV2 { amount currencyCode }
      subtotalPriceV2 { amount currencyCode }
      totalTaxV2 { amount currencyCode }
      shippingLine {
        handle
        title
        priceV2 { amount currencyCode }
      }
      availableShippingRates {
        ready
        shippingRates {
          handle
          title
          priceV2 { amount currencyCode }
        }
      }
      lineItems(first: 50) {
        edges {
          node {
            id
            title
            quantity
            variant {
              id
              title
              priceV2 { amount currencyCode }
              product {
                id
                title
                featuredImage { url }
              }
            }
          }
        }
      }
      shippingAddress {
        firstName lastName
        address1 address2
        city province country countryCodeV2 zip phone
      }
      discountApplications(first: 10) {
        edges {
          node {
            ... on DiscountCodeApplication {
              code
              applicable
              allocationMethod
              targetType
              value {
                ... on MoneyV2 { amount currencyCode }
                ... on PricingPercentageValue { percentage }
              }
            }
            ... on AutomaticDiscountApplication {
              title
              allocationMethod
              targetType
              value {
                ... on MoneyV2 { amount currencyCode }
                ... on PricingPercentageValue { percentage }
              }
            }
          }
        }
      }
      currencyCode
      requiresShipping
    }
  }
}`

const gqlCheckoutUpdate = `
mutation CheckoutUpdate($checkoutId: ID!, $input: CheckoutAttributesUpdateV2Input!) {
  checkoutAttributesUpdateV2(checkoutId: $checkoutId, input: $input) {
    checkout {
      id
      webUrl
      email
      totalPriceV2 { amount currencyCode }
      subtotalPriceV2 { amount currencyCode }
      totalTaxV2 { amount currencyCode }
      currencyCode
    }
    checkoutUserErrors { code field message }
  }
}`

const gqlCheckoutEmailUpdate = `
mutation CheckoutEmailUpdate($checkoutId: ID!, $email: String!) {
  checkoutEmailUpdateV2(checkoutId: $checkoutId, email: $email) {
    checkout { id email }
    checkoutUserErrors { code field message }
  }
}`

const gqlCheckoutShippingAddressUpdate = `
mutation CheckoutShippingAddressUpdate($checkoutId: ID!, $shippingAddress: MailingAddressInput!) {
  checkoutShippingAddressUpdateV2(checkoutId: $checkoutId, shippingAddress: $shippingAddress) {
    checkout {
      id
      shippingAddress {
        firstName lastName address1 address2 city province country zip phone
      }
      availableShippingRates {
        ready
        shippingRates {
          handle
          title
          priceV2 { amount currencyCode }
        }
      }
    }
    checkoutUserErrors { code field message }
  }
}`

const gqlCheckoutShippingLineUpdate = `
mutation CheckoutShippingLineUpdate($checkoutId: ID!, $shippingRateHandle: String!) {
  checkoutShippingLineUpdate(checkoutId: $checkoutId, shippingRateHandle: $shippingRateHandle) {
    checkout {
      id
      shippingLine {
        handle
        title
        priceV2 { amount currencyCode }
      }
      totalPriceV2 { amount currencyCode }
    }
    checkoutUserErrors { code field message }
  }
}`

const gqlCheckoutCompleteWithTokenizedPayment = `
mutation CheckoutCompleteWithTokenizedPaymentV3($checkoutId: ID!, $payment: TokenizedPaymentInputV3!) {
  checkoutCompleteWithTokenizedPaymentV3(checkoutId: $checkoutId, payment: $payment) {
    checkout {
      id
      completedAt
      webUrl
      order {
        id
        name
        statusUrl
      }
    }
    checkoutUserErrors { code field message }
    payment {
      id
      ready
      errorMessage
      transaction {
        status
        statusV2
      }
    }
  }
}`

// ─── Discount code mutations ──────────────────────────────────────────────────

const gqlCheckoutDiscountCodeApply = `
mutation CheckoutDiscountCodeApply($checkoutId: ID!, $discountCode: String!) {
  checkoutDiscountCodeApplyV2(checkoutId: $checkoutId, discountCode: $discountCode) {
    checkout {
      id
      discountApplications(first: 10) {
        edges {
          node {
            ... on DiscountCodeApplication {
              code
              applicable
              allocationMethod
              targetType
              value {
                ... on MoneyV2 { amount currencyCode }
                ... on PricingPercentageValue { percentage }
              }
            }
          }
        }
      }
    }
    checkoutUserErrors { code field message }
  }
}`

const gqlCheckoutDiscountCodeRemove = `
mutation CheckoutDiscountCodeRemove($checkoutId: ID!) {
  checkoutDiscountCodeRemove(checkoutId: $checkoutId) {
    checkout {
      id
      discountApplications(first: 10) {
        edges { node { ... on DiscountCodeApplication { code applicable } } }
      }
    }
    checkoutUserErrors { code field message }
  }
}`
