# Verifying a release

Signed releases include `checksums.txt.sig`, `checksums.txt.pem` and
`provenance.sigstore.json`. Older releases may not have these files.

With cosign 3 and the GitHub CLI installed, select a signed release tag and
archive from the [release assets](https://github.com/aramponi/yakanban/releases),
then verify before extracting or running it (Bash, macOS or Linux):

```bash
TAG=vX.Y.Z # replace with the release tag to verify
ARCHIVE="yakanban_${TAG#v}_linux_amd64.tar.gz" # choose your platform
mkdir -p "verify-$TAG"
cd "verify-$TAG"
gh release download "$TAG" --repo aramponi/yakanban \
  --pattern "$ARCHIVE" --pattern 'checksums.txt*' --pattern provenance.sigstore.json
cosign verify-blob checksums.txt \
  --signature checksums.txt.sig \
  --certificate checksums.txt.pem \
  --certificate-identity "https://github.com/aramponi/yakanban/.github/workflows/release.yml@refs/tags/$TAG" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
# Check the chosen archive against the authenticated manifest.
awk -v name="$ARCHIVE" '$2 == name { print; found=1 } END { if (!found) exit 1 }' \
  checksums.txt > selected-checksum.txt && shasum -a 256 -c selected-checksum.txt
gh attestation verify "$ARCHIVE" --repo aramponi/yakanban \
  --bundle provenance.sigstore.json \
  --signer-workflow aramponi/yakanban/.github/workflows/release.yml \
  --source-ref "refs/tags/$TAG"
```

All three checks must succeed. The exact certificate identity binds the
signature to the release workflow and selected tag; the signed checksum binds
the archive to that manifest. The SLSA build attestation records the source
commit and build workflow. This is supply-chain verification, not Authenticode
signing or macOS Developer ID signing/notarization; OS trust warnings are
unchanged.

CI checks the release security configuration and builds a snapshot on pull
requests. CI explicitly skips snapshot signing and does not prove OIDC works.
Each real tag run also downloads and verifies the published signatures,
checksums and provenance; that run must pass before treating a new release as
verified.

## Homebrew and the trusted tap

The Homebrew command in the README is deliberately the fully qualified one: it
trusts that single cask on the spot. Homebrew 6 requires third-party taps to be
trusted, so the short form needs one extra step:

```bash
brew tap aramponi/tap
brew trust --cask aramponi/tap/yakanban
brew install yakanban
```

Each packaged route covers one platform: Homebrew ships a cask, which is a
macOS mechanism, and the Scoop bucket carries the Windows build only — amd64,
since `windows/arm64` is not built.

Neither packager runs the first-login step for you, and Scoop has no equivalent
of the cask's caveats to remind you. After installing, once:

```powershell
gh auth login
gh auth refresh -s project   # Projects v2 needs its own scope
```

## As a GitHub CLI extension

The same binary answers to `gh yakanban ...` when it is installed under the
name `gh-yakanban` — help output adapts automatically:

```bash
make gh-extension     # builds and installs into gh's extension directory
gh yakanban board
```

This is a local build, not a release artifact. `gh extension install` resolves
a *repository* whose name begins with `gh-`, which this one does not, so a
published `gh-yakanban` binary could never be installed the documented way.
Shipping one would have been dead weight in every release.
