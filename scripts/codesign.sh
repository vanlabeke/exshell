#!/usr/bin/env bash
#
# Code-sign a macOS binary with a Developer ID Application certificate.
#
# Called as a GoReleaser post-build hook on the universal darwin binary. It is
# a deliberate no-op when MACOS_SIGN_IDENTITY is unset, so that local snapshot
# builds and CI runs without secrets (forks, pull requests) still succeed —
# they just produce unsigned binaries.
#
# Usage: scripts/codesign.sh <path-to-binary>
set -euo pipefail

binary=${1:?usage: codesign.sh <binary>}

if [ -z "${MACOS_SIGN_IDENTITY:-}" ]; then
	# Same reasoning as notarize.sh: a release that quietly skips signing
	# ships a binary Gatekeeper blocks, and nothing in the log says so.
	# SNAPSHOT comes from the GoReleaser hook as {{ .IsSnapshot }}; anything
	# but "true" is treated as a release and must fail.
	if [ "${SNAPSHOT:-}" != "true" ]; then
		echo "codesign: MACOS_SIGN_IDENTITY unset for a release build" >&2
		echo "codesign: refusing to ship an unsigned binary" >&2
		exit 1
	fi
	echo "codesign: snapshot build, leaving $binary unsigned" >&2
	exit 0
fi

if ! command -v codesign >/dev/null 2>&1; then
	echo "codesign: not available on this host (not macOS?)" >&2
	exit 1
fi

# Refuse anything that is not a Developer ID Application certificate.
#
# An "Apple Development" or "Apple Distribution" certificate signs perfectly
# happily, and the result then fails everywhere it matters: Apple's notary
# service will not notarize it, and Gatekeeper rejects it on every Mac except
# the ones registered to that development profile. That failure surfaces to
# users as "Apple could not verify this app is free of malware", long after
# the release has shipped — so it has to be fatal here, at build time.
case "$MACOS_SIGN_IDENTITY" in
"Developer ID Application:"*) ;;
*)
	echo "codesign: refusing to sign with '$MACOS_SIGN_IDENTITY'" >&2
	echo "codesign: distribution outside the App Store requires a" >&2
	echo "          'Developer ID Application' certificate." >&2
	echo "          Create one at:" >&2
	echo "          https://developer.apple.com/account/resources/certificates/add" >&2
	exit 1
	;;
esac

# --options runtime enables the hardened runtime, which notarization requires.
# --timestamp gets a trusted timestamp so the signature outlives the
# certificate's expiry.
codesign \
	--force \
	--options runtime \
	--timestamp \
	--sign "$MACOS_SIGN_IDENTITY" \
	"$binary"

# Verify rather than trust the exit code: codesign can succeed while producing
# a signature Gatekeeper will not accept.
codesign --verify --strict --verbose=2 "$binary"

echo "codesign: signed $binary" >&2
