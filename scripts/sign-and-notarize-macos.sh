#!/usr/bin/env bash
# Sign Grain.app with Developer ID Application and notarize + staple.
#
# Required env (local or GitHub Actions secrets):
#   APPLE_TEAM_ID
#   MACOS_CERTIFICATE          — base64-encoded .p12 (Developer ID Application + key)
#   MACOS_CERTIFICATE_PWD      — .p12 password
#   APPLE_API_KEY_ID
#   APPLE_API_ISSUER_ID
#   APPLE_API_KEY              — base64 of AuthKey_*.p8 OR raw PEM contents
#
# Optional:
#   APP_PATH                   — default desktop/build/bin/Grain.app
#   APPLE_SIGN_IDENTITY        — default "Developer ID Application: * (TEAM_ID)"
#   ENTITLEMENTS               — default desktop/entitlements.plist
#
# Usage (repo root):
#   export APPLE_TEAM_ID=… MACOS_CERTIFICATE=… # etc
#   ./scripts/sign-and-notarize-macos.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "sign-and-notarize-macos: macOS only" >&2
  exit 1
fi

APP_PATH="${APP_PATH:-${ROOT}/desktop/build/bin/Grain.app}"
ENTITLEMENTS="${ENTITLEMENTS:-${ROOT}/desktop/entitlements.plist}"

require() {
  local n="$1"
  if [[ -z "${!n:-}" ]]; then
    echo "error: missing required env ${n}" >&2
    exit 1
  fi
}

require APPLE_TEAM_ID
require MACOS_CERTIFICATE
require MACOS_CERTIFICATE_PWD
require APPLE_API_KEY_ID
require APPLE_API_ISSUER_ID
require APPLE_API_KEY

if [[ ! -d "$APP_PATH" ]]; then
  echo "error: app not found: ${APP_PATH} (run just desktop-build first)" >&2
  exit 1
fi
if [[ ! -f "$ENTITLEMENTS" ]]; then
  echo "error: entitlements not found: ${ENTITLEMENTS}" >&2
  exit 1
fi

KEYCHAIN_PATH="${RUNNER_TEMP:-${TMPDIR:-/tmp}}/grain-signing.keychain-db"
KEYCHAIN_PWD="$(openssl rand -base64 32)"
CERT_PATH="${RUNNER_TEMP:-${TMPDIR:-/tmp}}/grain-dev-id.p12"
API_KEY_PATH="${RUNNER_TEMP:-${TMPDIR:-/tmp}}/AuthKey_${APPLE_API_KEY_ID}.p8"
ZIP_PATH="${RUNNER_TEMP:-${TMPDIR:-/tmp}}/Grain-notarize.zip"

cleanup() {
  security delete-keychain "$KEYCHAIN_PATH" 2>/dev/null || true
  rm -f "$CERT_PATH" "$API_KEY_PATH" "$ZIP_PATH"
}
trap cleanup EXIT

echo "sign-and-notarize: preparing keychain…"
# Decode .p12
if printf '%s' "$MACOS_CERTIFICATE" | base64 --decode >"$CERT_PATH" 2>/dev/null; then
  :
elif printf '%s' "$MACOS_CERTIFICATE" | base64 -D >"$CERT_PATH" 2>/dev/null; then
  :
else
  echo "error: MACOS_CERTIFICATE is not valid base64" >&2
  exit 1
fi

# Decode or write API key (.p8 may be base64 or raw PEM)
if printf '%s' "$APPLE_API_KEY" | grep -q "BEGIN PRIVATE KEY"; then
  printf '%s\n' "$APPLE_API_KEY" >"$API_KEY_PATH"
else
  if ! printf '%s' "$APPLE_API_KEY" | base64 --decode >"$API_KEY_PATH" 2>/dev/null \
    && ! printf '%s' "$APPLE_API_KEY" | base64 -D >"$API_KEY_PATH" 2>/dev/null; then
    echo "error: APPLE_API_KEY must be PEM or base64 of .p8" >&2
    exit 1
  fi
fi
chmod 600 "$API_KEY_PATH" "$CERT_PATH"

security create-keychain -p "$KEYCHAIN_PWD" "$KEYCHAIN_PATH"
security set-keychain-settings -lut 21600 "$KEYCHAIN_PATH"
security unlock-keychain -p "$KEYCHAIN_PWD" "$KEYCHAIN_PATH"
security import "$CERT_PATH" \
  -P "$MACOS_CERTIFICATE_PWD" \
  -A \
  -t cert \
  -f pkcs12 \
  -k "$KEYCHAIN_PATH"
security list-keychain -d user -s "$KEYCHAIN_PATH" $(security list-keychain -d user | tr -d '"')
security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$KEYCHAIN_PWD" "$KEYCHAIN_PATH"

IDENTITY="${APPLE_SIGN_IDENTITY:-}"
if [[ -z "$IDENTITY" ]]; then
  IDENTITY="$(security find-identity -v -p codesigning "$KEYCHAIN_PATH" \
    | grep "Developer ID Application" \
    | grep "$APPLE_TEAM_ID" \
    | head -1 \
    | sed -E 's/.*"([^"]+)".*/\1/' || true)"
fi
if [[ -z "$IDENTITY" ]]; then
  echo "error: no Developer ID Application identity for team ${APPLE_TEAM_ID}" >&2
  security find-identity -v -p codesigning "$KEYCHAIN_PATH" || true
  exit 1
fi
echo "sign-and-notarize: identity=${IDENTITY}"

# Clear ad-hoc signature and re-sign with hardened runtime.
xattr -cr "$APP_PATH" 2>/dev/null || true
codesign --force --deep --options runtime \
  --entitlements "$ENTITLEMENTS" \
  --sign "$IDENTITY" \
  "$APP_PATH"
codesign --verify --deep --strict --verbose=2 "$APP_PATH"

echo "sign-and-notarize: submitting to notary…"
# Zip for notarytool (Apple requires zip/dmg/pkg, not raw .app).
rm -f "$ZIP_PATH"
ditto -c -k --keepParent "$APP_PATH" "$ZIP_PATH"

xcrun notarytool submit "$ZIP_PATH" \
  --key "$API_KEY_PATH" \
  --key-id "$APPLE_API_KEY_ID" \
  --issuer "$APPLE_API_ISSUER_ID" \
  --wait

echo "sign-and-notarize: stapling…"
xcrun stapler staple "$APP_PATH"
xcrun stapler validate "$APP_PATH" || true
spctl --assess --type execute --verbose "$APP_PATH" || true

echo "sign-and-notarize: done → ${APP_PATH}"
