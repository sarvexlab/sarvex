#!/usr/bin/env bash
# Mints an unsigned dev bearer token accepted by the control plane's
# SimBearerResolver (internal/planes/control/auth/bearer.go), active only
# when KINDE_ISSUER_URL is unset. It's a JWT-shaped header.payload.signature
# string whose payload is inspected but never signature-verified — dev/local
# only, never valid against a real deployment.
#
# Usage: scripts/dev/token.sh [--merchant-id ID] [--role ROLE] [--sub SUB] [--email EMAIL]
# Prints the token to stdout (nothing else), so it composes as:
#   TOKEN=$(scripts/dev/token.sh --merchant-id merch-dev-1)
set -euo pipefail

merchant_id="merch-dev-1"
role="owner"
sub="dev-user-1"
email="dev@example.com"

while [ $# -gt 0 ]; do
	case "$1" in
	--merchant-id)
		merchant_id="$2"
		shift 2
		;;
	--role)
		role="$2"
		shift 2
		;;
	--sub)
		sub="$2"
		shift 2
		;;
	--email)
		email="$2"
		shift 2
		;;
	*)
		echo "unknown argument: $1" >&2
		exit 1
		;;
	esac
done

if ! command -v python3 >/dev/null 2>&1; then
	echo "python3 is required to mint a dev token" >&2
	exit 1
fi

python3 - "$merchant_id" "$role" "$sub" "$email" <<'EOF'
import base64, json, sys

merchant_id, role, sub, email = sys.argv[1:5]

def b64url(d):
    return base64.urlsafe_b64encode(d).rstrip(b'=').decode()

header = b64url(json.dumps({"alg": "none"}).encode())
claims = {"sub": sub, "email": email, "role": role, "store_ids": []}
if role != "operator":
    claims["merchant_id"] = merchant_id
payload = b64url(json.dumps(claims).encode())
print(f"{header}.{payload}.sig")
EOF
