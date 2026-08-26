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
	echo "codesign: MACOS_SIGN_IDENTITY unset, leaving $binary unsigned" >&2
	exit 0
fi

if ! command -v codesign >/dev/null 2>&1; then
	echo "codesign: not available on this host (not macOS?)" >&2
	exit 1
fi

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
