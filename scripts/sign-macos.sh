#!/usr/bin/env bash
# GoReleaser post-build hook: sign before archives/checksums/provenance exist.
set -euo pipefail

if [[ "${RELEASE_GOOS:?missing target OS}" != darwin || "${RELEASE_SNAPSHOT:?missing snapshot flag}" == true ]]; then
  exit 0
fi

: "${MACOS_SIGN_IDENTITY:?missing Developer ID Application identity}"
: "${MACOS_KEYCHAIN:?missing release keychain}"
: "${MACOS_NOTARY_PROFILE:?missing notarization profile}"
binary="${1:?missing binary path}"

codesign --force --timestamp --options runtime \
  --keychain "$MACOS_KEYCHAIN" --sign "$MACOS_SIGN_IDENTITY" "$binary"
codesign --verify --strict "$binary"

# Apple's service accepts ZIP, not tar.gz. The final archives contain these
# exact signed bytes. Standalone Mach-O files cannot have a ticket stapled.
notary_dir="$(mktemp -d)"
trap 'rm -rf "$notary_dir"' EXIT
ditto -c -k --keepParent "$binary" "$notary_dir/submission.zip"
if ! xcrun notarytool submit "$notary_dir/submission.zip" \
  --keychain "$MACOS_KEYCHAIN" --keychain-profile "$MACOS_NOTARY_PROFILE" \
  --wait --timeout 20m --output-format json > "$notary_dir/result.json"; then
  cat "$notary_dir/result.json" >&2
  echo 'Apple notarization failed or timed out; no release will be published.' >&2
  exit 1
fi
if ! jq -e '.status == "Accepted"' "$notary_dir/result.json" > /dev/null; then
  cat "$notary_dir/result.json" >&2
  echo 'Apple did not accept this binary; no release will be published.' >&2
  exit 1
fi
cat "$notary_dir/result.json"
