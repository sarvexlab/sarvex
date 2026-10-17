// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package ucpsig

import (
	"crypto/ecdsa"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

// ECPublicKeyToJWK converts an ECDSA P-256 public key to a UCP JWK.
// kid is the key identifier to embed in the JWK.
func ECPublicKeyToJWK(pubKey *ecdsa.PublicKey, kid string) *wire.JWK {
	xBytes := make([]byte, 32)
	yBytes := make([]byte, 32)
	pubKey.X.FillBytes(xBytes)
	pubKey.Y.FillBytes(yBytes)

	return &wire.JWK{
		Kid: kid,
		Kty: "EC",
		Crv: "P-256",
		X:   base64.RawURLEncoding.EncodeToString(xBytes),
		Y:   base64.RawURLEncoding.EncodeToString(yBytes),
		Use: "sig",
		Alg: "ES256", // RFC 7518 name; case-sensitive (RFC 7517 §4.4)
	}
}

// MessageDirection distinguishes request signatures (which use @method) from
// response signatures (which use @status).
type MessageDirection int

// VerifiedSignature is what's placed on the request context after successful
// verification.
type VerifiedSignature struct {
	Params     SignatureParams
	ProfileURL string
}

const (
	DirectionRequest MessageDirection = iota
	DirectionResponse
)

// SignatureParams is the parsed form of the value associated with a label in
// a Signature-Input header (e.g. for label "sig1").
type SignatureParams struct {
	Label      string   // dictionary label, conventionally "sig1"
	Components []string // ordered list of component identifiers
	KeyID      string   // keyid parameter (required by UCP)
	Created    int64    // optional; 0 if absent
	Expires    int64    // optional; 0 if absent
	Nonce      string   // optional
	Alg        string   // optional; UCP derives alg from key's curve
}

// BuildSignatureBase builds the RFC 9421 signature base string for the given
// component list and parameter set. The base is the concatenation of one line
// per component followed by the @signature-params line.
//
// For request signatures, set direction=DirectionRequest and pass req. For
// response signatures, set direction=DirectionResponse and pass status,
// respHeaders, and (optionally) req for any derived components that reference
// the original request.
func BuildSignatureBase(
	direction MessageDirection,
	req *http.Request,
	status int,
	respHeaders http.Header,
	params SignatureParams,
) (string, error) {
	var b strings.Builder
	for _, comp := range params.Components {
		line, err := componentLine(comp, direction, req, status, respHeaders)
		if err != nil {
			return "", err
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	// Final line: "@signature-params": <serialized params inner list>
	b.WriteString(`"@signature-params": `)
	b.WriteString(serializeSignatureParams(params))
	return b.String(), nil
}

// serializeSignatureParams renders the inner-list value of a Signature-Input
// dictionary entry (everything after `sig1=`).
func serializeSignatureParams(p SignatureParams) string {
	var b strings.Builder
	b.WriteByte('(')
	for i, c := range p.Components {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteByte('"')
		b.WriteString(c)
		b.WriteByte('"')
	}
	b.WriteByte(')')
	if p.Created != 0 {
		b.WriteString(";created=")
		b.WriteString(strconv.FormatInt(p.Created, 10))
	}
	if p.Expires != 0 {
		b.WriteString(";expires=")
		b.WriteString(strconv.FormatInt(p.Expires, 10))
	}
	if p.Nonce != "" {
		b.WriteString(`;nonce="`)
		b.WriteString(p.Nonce)
		b.WriteByte('"')
	}
	if p.Alg != "" {
		b.WriteString(`;alg="`)
		b.WriteString(p.Alg)
		b.WriteByte('"')
	}
	if p.KeyID != "" {
		b.WriteString(`;keyid="`)
		b.WriteString(p.KeyID)
		b.WriteByte('"')
	}
	return b.String()
}

// componentLine renders one line of the signature base.
func componentLine(
	comp string,
	direction MessageDirection,
	req *http.Request,
	status int,
	respHeaders http.Header,
) (string, error) {
	switch comp {
	case "@method":
		if direction != DirectionRequest || req == nil {
			return "", fmt.Errorf("ucpsig: @method requires a request")
		}
		return `"@method": ` + strings.ToUpper(req.Method), nil
	case "@authority":
		if req == nil {
			return "", fmt.Errorf("ucpsig: @authority requires a request")
		}
		host := req.Host
		if host == "" && req.URL != nil {
			host = req.URL.Host
		}
		return `"@authority": ` + strings.ToLower(host), nil
	case "@path":
		if req == nil || req.URL == nil {
			return "", fmt.Errorf("ucpsig: @path requires a request URL")
		}
		path := req.URL.EscapedPath()
		if path == "" {
			path = "/"
		}
		return `"@path": ` + path, nil
	case "@query":
		if req == nil || req.URL == nil {
			return "", fmt.Errorf("ucpsig: @query requires a request URL")
		}
		raw := req.URL.RawQuery
		// Per RFC 9421, the @query value includes the leading "?". If absent,
		// the value is "?".
		return `"@query": ?` + raw, nil
	case "@status":
		if direction != DirectionResponse {
			return "", fmt.Errorf("ucpsig: @status only valid for responses")
		}
		return `"@status": ` + strconv.Itoa(status), nil
	default:
		// Plain HTTP field. RFC 9421 lower-cases the field name. Multiple
		// values are joined with ", " after trimming.
		if strings.HasPrefix(comp, "@") {
			return "", fmt.Errorf("ucpsig: unsupported derived component %q", comp)
		}
		var headers http.Header
		if direction == DirectionResponse {
			headers = respHeaders
		} else if req != nil {
			headers = req.Header
		}
		vals := headers.Values(http.CanonicalHeaderKey(comp))
		if len(vals) == 0 {
			return "", fmt.Errorf("ucpsig: signed header %q absent from message", comp)
		}
		trimmed := make([]string, len(vals))
		for i, v := range vals {
			trimmed[i] = strings.TrimSpace(v)
		}
		return `"` + strings.ToLower(comp) + `": ` + strings.Join(trimmed, ", "), nil
	}
}

// ParseSignatureInput parses a Signature-Input header value and returns the
// parameters for the given label (typically "sig1").
//
// Example input: sig1=("@method" "@authority" "@path");keyid="k1";created=123
func ParseSignatureInput(headerValue, label string) (SignatureParams, error) {
	// We accept a dictionary with one or more entries. The grammar in RFC 8941
	// is significantly richer, but UCP's profile only emits this shape, so a
	// targeted parser is sufficient and easier to audit.
	var out SignatureParams
	entries, err := splitDictEntries(headerValue)
	if err != nil {
		return out, err
	}
	for _, e := range entries {
		key, val, ok := splitOnce(e, "=")
		if !ok {
			return out, fmt.Errorf("ucpsig: malformed Signature-Input entry %q", e)
		}
		key = strings.TrimSpace(key)
		if key != label {
			continue
		}
		return parseInnerList(label, val)
	}
	return out, fmt.Errorf("ucpsig: label %q not found in Signature-Input", label)
}

// ParseSignature parses a Signature header value and returns the raw signature
// bytes for the given label.
func ParseSignature(headerValue, label string) ([]byte, error) {
	entries, err := splitDictEntries(headerValue)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		key, val, ok := splitOnce(e, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) != label {
			continue
		}
		val = strings.TrimSpace(val)
		// Expect format :base64:
		if len(val) < 2 || val[0] != ':' || val[len(val)-1] != ':' {
			return nil, fmt.Errorf("ucpsig: malformed Signature value for %q", label)
		}
		return decodeStdBase64(val[1 : len(val)-1])
	}
	return nil, fmt.Errorf("ucpsig: label %q not found in Signature header", label)
}

// parseInnerList parses `("a" "b");p1=v1;p2="v2"` into a SignatureParams.
func parseInnerList(label, val string) (SignatureParams, error) {
	val = strings.TrimSpace(val)
	if !strings.HasPrefix(val, "(") {
		return SignatureParams{}, fmt.Errorf("ucpsig: Signature-Input value must start with '('")
	}
	end := strings.Index(val, ")")
	if end == -1 {
		return SignatureParams{}, fmt.Errorf("ucpsig: missing closing ')'")
	}
	inner := val[1:end]
	rest := val[end+1:]

	params := SignatureParams{Label: label}
	for _, tok := range tokenizeQuoted(inner) {
		params.Components = append(params.Components, tok)
	}

	// rest is a sequence of ;name=value pairs.
	for rest != "" {
		rest = strings.TrimSpace(rest)
		if !strings.HasPrefix(rest, ";") {
			break
		}
		rest = rest[1:]
		semi := strings.IndexAny(rest, ";")
		var pair string
		if semi == -1 {
			pair, rest = rest, ""
		} else {
			pair, rest = rest[:semi], rest[semi:]
		}
		k, v, ok := splitOnce(pair, "=")
		if !ok {
			return SignatureParams{}, fmt.Errorf("ucpsig: malformed parameter %q", pair)
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"`)
		switch strings.ToLower(k) {
		case "keyid":
			params.KeyID = v
		case "created":
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return SignatureParams{}, fmt.Errorf("ucpsig: invalid created: %w", err)
			}
			params.Created = n
		case "expires":
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return SignatureParams{}, fmt.Errorf("ucpsig: invalid expires: %w", err)
			}
			params.Expires = n
		case "nonce":
			params.Nonce = v
		case "alg":
			params.Alg = v
		}
	}
	return params, nil
}

// splitDictEntries splits an RFC 8941 dictionary on commas not inside quoted
// strings or balanced parens or byte-sequence colons.
func splitDictEntries(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	depth := 0
	inQuote := false
	inBytes := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' && !inBytes:
			inQuote = !inQuote
			cur.WriteByte(c)
		case c == ':' && !inQuote:
			inBytes = !inBytes
			cur.WriteByte(c)
		case c == '(' && !inQuote && !inBytes:
			depth++
			cur.WriteByte(c)
		case c == ')' && !inQuote && !inBytes:
			depth--
			cur.WriteByte(c)
		case c == ',' && depth == 0 && !inQuote && !inBytes:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out, nil
}

func splitOnce(s, sep string) (string, string, bool) {
	i := strings.Index(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+len(sep):], true
}

func tokenizeQuoted(s string) []string {
	var out []string
	var cur strings.Builder
	in := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			if in {
				out = append(out, cur.String())
				cur.Reset()
			}
			in = !in
		default:
			if in {
				cur.WriteByte(c)
			}
		}
	}
	return out
}

// ParseUCPAgent parses an RFC 8941 dictionary value of the form:
//
//	profile="https://platform.example/.well-known/ucp"
//
// It returns the profile URL or an error if missing/invalid. allowInsecureHTTP,
// when true, allows the profile URL to use http:// instead of https://. This
// should only be set to true for local testing; production code MUST use false.
func ParseUCPAgent(headerValue string, allowInsecureHTTP bool) (string, error) {
	entries, err := splitDictEntries(headerValue)
	if err != nil {
		return "", ErrInvalidProfileURL.withCause(err)
	}
	for _, e := range entries {
		k, v, ok := splitOnce(e, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(k) != "profile" {
			continue
		}
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"`)
		u, err := url.Parse(v)
		if err != nil {
			return "", ErrInvalidProfileURL.withCause(err)
		}
		if u.Scheme != "https" && (!allowInsecureHTTP || u.Scheme != "http") {
			return "", ErrInvalidProfileURL.withCause(fmt.Errorf("scheme must be https, got %q", u.Scheme))
		}
		return v, nil
	}
	return "", ErrInvalidProfileURL.withCause(fmt.Errorf("UCP-Agent missing profile parameter"))
}
