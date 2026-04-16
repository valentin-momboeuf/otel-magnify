#!/usr/bin/env bash
# Release script for otel-magnify.
#
# Injects a rolling BSL Change Date (release date + 4 years) into LICENSE,
# commits and tags the release, then restores the {{CHANGE_DATE}} placeholder
# for the next release.
#
# Usage: scripts/release.sh vX.Y.Z
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "Usage: $0 <version>"
  echo "Example: $0 v0.1.0"
  exit 1
fi

version="$1"

if ! [[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Error: version must match vX.Y.Z (got: $version)"
  exit 1
fi

# Refuse to proceed if the working tree has uncommitted changes.
if ! git diff --quiet HEAD --; then
  echo "Error: working tree is not clean. Commit or stash changes first."
  exit 1
fi

# Refuse to proceed if the placeholder is missing.
if ! grep -q '{{CHANGE_DATE}}' LICENSE; then
  echo "Error: LICENSE does not contain {{CHANGE_DATE}} placeholder."
  echo "Either the placeholder was already substituted or LICENSE has been edited manually."
  exit 1
fi

# Compute Change Date = today + 4 years (BSD/macOS or GNU/Linux date).
if change_date=$(date -v+4y +%Y-%m-%d 2>/dev/null); then
  :
else
  change_date=$(date -d "+4 years" +%Y-%m-%d)
fi

echo "Release ${version}: setting BSL Change Date to ${change_date}"

# Substitute the placeholder.
if [[ "$(uname)" == "Darwin" ]]; then
  sed -i '' "s|{{CHANGE_DATE}}|${change_date}|g" LICENSE
else
  sed -i "s|{{CHANGE_DATE}}|${change_date}|g" LICENSE
fi

git add LICENSE
git commit -m "release: ${version} (BSL Change Date ${change_date})"
git tag -a "${version}" -m "release: ${version}"

# Restore the placeholder so future commits start from a templated LICENSE.
if [[ "$(uname)" == "Darwin" ]]; then
  sed -i '' "s|${change_date}|{{CHANGE_DATE}}|g" LICENSE
else
  sed -i "s|${change_date}|{{CHANGE_DATE}}|g" LICENSE
fi

git add LICENSE
git commit -m "chore: restore BSL Change Date placeholder after ${version}"

cat <<EOF

Release ${version} prepared locally.
- Tag: ${version}
- BSL Change Date for this release: ${change_date}

Next steps:
  git push origin main
  git push origin ${version}
EOF
