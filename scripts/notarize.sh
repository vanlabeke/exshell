#!/usr/bin/env bash
#
# Notarize a signed macOS binary with Apple's notary service.
#
# Called as a GoReleaser post-build hook on the universal darwin binary,
# immediately after scripts/codesign.sh.
#
# Why notarize the bare binary rather than a published archive: Apple
# registers the *signed binary's* code-directory hash, and Gatekeeper looks
# that hash up online. The ticket therefore follows the binary into whatever
# container ships it, so notarizing here covers every tarball built from it.
# Stapling — which would embed the ticket locally — only works on .app/.dmg/
# .pkg bundles, so it is not applicable to a bare CLI binary at all.
#
# notarytool needs a zip/dmg/pkg to submit, so this wraps the binary in a
# throwaway zip that is never published.
#
# A deliberate no-op when the App Store Connect credentials are absent, so
# local snapshot builds and unsecreted CI runs still succeed unnotarized.
#
# Usage: scripts/notarize.sh <path-to-binary>
set -euo pipefail

binary=${1:?usage: notarize.sh <binary>}

if [ -z "${AC_KEY_ID:-}" ] || [ -z "${AC_ISSUER_ID:-}" ] || [ -z "${AC_KEY_PATH:-}" ]; then
	echo "notarize: App Store Connect credentials unset, skipping $binary" >&2
	exit 0
fi

if ! command -v xcrun >/dev/null 2>&1; then
	echo "notarize: xcrun unavailable (not macOS?)" >&2
	exit 1
fi

tmpzip=$(mktemp -t exshell-notarize).zip
cleanup() { rm -f "$tmpzip"; }
trap cleanup EXIT

# ditto, not zip(1): ditto preserves the code signature and extended
# attributes that notarization inspects.
ditto -c -k --keepParent "$binary" "$tmpzip"

echo "notarize: submitting $binary (this waits for Apple, typically 1-5 min)" >&2

# --wait blocks until Apple returns Accepted or Invalid, so a rejected
# submission fails the build rather than silently shipping an unnotarized
# binary.
xcrun notarytool submit "$tmpzip" \
	--key "$AC_KEY_PATH" \
	--key-id "$AC_KEY_ID" \
	--issuer "$AC_ISSUER_ID" \
	--wait \
	--timeout 30m

echo "notarize: accepted for $binary" >&2
