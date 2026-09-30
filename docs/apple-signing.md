# Apple signing setup for maintainers

The release workflow requires an active Apple Developer Program membership, a
**Developer ID Application** certificate with its private key, and an App Store
Connect team API key for notarization. An **Apple Development** certificate does
not qualify for distribution outside the Mac App Store.

## 1. Create and export the signing identity

In Xcode, open Settings → Accounts, select your Apple account and team, open
Manage Certificates, then use + → Developer ID Application. This requires the
Account Holder role. If you already have this identity, reuse it.

Alternatively, use Apple's [certificate creation guide](https://developer.apple.com/help/account/certificates/create-developer-id-certificates/):
create a certificate signing request in Keychain Access, upload it in
Certificates, Identifiers & Profiles, select Developer ID Application, download
the certificate and import it on the same Mac that generated the request.

In Keychain Access → My Certificates, find the Developer ID Application identity
and expand it to confirm its private key is present. Export that identity and
private key as a password-protected `.p12` file. Export only this identity;
the workflow rejects a P12 containing zero or multiple usable Developer ID
Application identities.

## 2. Create a notarization key

In App Store Connect → Users and Access → Integrations → App Store Connect API,
create a **team** API key with the Developer role (or a role with equivalent
notarization access). Record its Key ID and Issuer ID and download the `.p8`
private key. Apple only offers this download once. Individual keys do not have
an Issuer ID and are not supported by this workflow.

See Apple's [notarization workflow documentation](https://developer.apple.com/documentation/security/customizing-the-notarization-workflow).
No App Store product listing or app submission is required for this CLI.

## 3. Configure GitHub Actions secrets

Set these repository secrets for `aramponi/yakanban`:

| Secret | Value |
| --- | --- |
| `MACOS_SIGN_P12` | Base64-encoded P12 identity (certificate and private key) |
| `MACOS_SIGN_PASSWORD` | Password chosen when exporting the P12 |
| `MACOS_NOTARY_KEY` | Base64-encoded App Store Connect `.p8` key |
| `MACOS_NOTARY_KEY_ID` | Key ID shown by App Store Connect |
| `MACOS_NOTARY_ISSUER_ID` | Team key's Issuer ID |

Run from your own terminal, substituting the paths. These commands pipe the
encoded files directly to GitHub; do not paste their contents into chat, source
files, issue comments or workflow logs.

```bash
base64 -i /path/to/DeveloperID.p12 | gh secret set MACOS_SIGN_P12 --repo aramponi/yakanban
base64 -i /path/to/AuthKey_KEYID.p8 | gh secret set MACOS_NOTARY_KEY --repo aramponi/yakanban
gh secret set MACOS_SIGN_PASSWORD --repo aramponi/yakanban
gh secret set MACOS_NOTARY_KEY_ID --repo aramponi/yakanban
gh secret set MACOS_NOTARY_ISSUER_ID --repo aramponi/yakanban
```

The last three commands prompt for the value. The workflow imports the identity
into a temporary keychain, validates notarization credentials with Apple, and
removes the keychain and temporary key files even when a step fails. It does not
require a GoReleaser Pro license. Never commit P12 or P8 files to the repository.

## 4. Validate before considering a release complete

Do not merge this setup until the secrets are configured. A tag release with
missing or invalid Apple credentials fails before publishing. PR snapshots
remain unsigned, need no Apple credentials and are not notarization proof.

For each macOS architecture, the GoReleaser post-build hook signs with a secure
timestamp and hardened runtime, submits a temporary ZIP to Apple, waits up to
20 minutes and requires status `Accepted`. Archives, checksums and SLSA
provenance are generated only after both hooks succeed. A rejection or timeout
stops publication; inspect the submission ID in the workflow output and query
its notary log before retrying. A timeout can mean Apple is still processing.

After an explicitly approved real tag release, require all release checks to
pass. Download each macOS archive, verify it as described in
[Verifying a release](verifying-releases.md), then test the applicable binary
on a Mac with normal download quarantine intact. Do not use `xattr -d` as part
of acceptance testing. Record the tag, workflow run and Apple submission IDs in
the ticket. The first notarization may take longer than subsequent submissions.

A standalone Mach-O executable cannot have a ticket stapled. Gatekeeper can
retrieve its ticket online; fully offline first-run distribution would require
a separately notarized/stapled container such as a package, outside this change.
