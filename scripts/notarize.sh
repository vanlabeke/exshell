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
	# Skipping is right for a local snapshot, and wrong for a release: a
	# silent skip in CI ships an unnotarized binary that Gatekeeper blocks,
	# and the only clue is that this step returned suspiciously fast.
	if [ -n "${CI:-}" ]; then
		echo "notarize: App Store Connect credentials missing in CI" >&2
		echo "notarize:   AC_KEY_ID=${AC_KEY_ID:-<unset>}" >&2
		echo "notarize:   AC_ISSUER_ID=${AC_ISSUER_ID:+<set>}${AC_ISSUER_ID:-<unset>}" >&2
		echo "notarize:   AC_KEY_PATH=${AC_KEY_PATH:-<unset>}" >&2
		echo "notarize: refusing to ship an unnotarized binary" >&2
		exit 1
	fi
	echo "notarize: App Store Connect credentials unset, skipping $binary" >&2
	exit 0
fi

if [ ! -s "$AC_KEY_PATH" ]; then
	echo "notarize: AC_KEY_PATH points at nothing readable: $AC_KEY_PATH" >&2
	exit 1
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

# --wait blocks until Apple reaches a verdict.
#
# The verdict is then checked explicitly rather than relying on the exit code:
# notarytool has historically exited 0 on an Invalid submission, which would
# ship an unnotarized binary from a build that reported success. This is also
# the only place notarization can be verified at all — the ticket lives on
# Apple's servers, and a bare binary cannot be stapled, so no later local
# check can confirm it.
out=$(xcrun notarytool submit "$tmpzip" \
	--key "$AC_KEY_PATH" \
	--key-id "$AC_KEY_ID" \
	--issuer "$AC_ISSUER_ID" \
	--wait \
	--timeout 30m 2>&1) || true

printf '%s\n' "$out" >&2

if ! printf '%s' "$out" | grep -q "status: Accepted"; then
	echo "notarize: Apple did not accept $binary" >&2
	# The log holds the actual reason (unsigned nested code, missing
	# hardened runtime, a disallowed entitlement); fetch it rather than
	# leaving someone to guess.
	sub=$(printf '%s' "$out" | awk '/id: /{ print $2; exit }')
	if [ -n "$sub" ]; then
		echo "notarize: fetching the rejection log for submission $sub" >&2
		xcrun notarytool log "$sub" \
			--key "$AC_KEY_PATH" \
			--key-id "$AC_KEY_ID" \
			--issuer "$AC_ISSUER_ID" >&2 2>/dev/null || true
	fi
	exit 1
fi

echo "notarize: accepted for $binary" >&2
