#!/usr/bin/env bash
#
# Build signed APT and YUM repositories from a directory of .deb/.rpm files,
# into a tree suitable for static hosting on GitHub Pages.
#
# Usage: scripts/build-repos.sh <packages-dir> <repo-root>
#
#   packages-dir  directory containing the .deb and .rpm files for this release
#   repo-root     checkout of the Pages repo; existing packages are preserved
#
# Requires (Debian/Ubuntu): dpkg-dev apt-utils createrepo-c gnupg
#
# Signing: set GPG_KEY_ID to sign repository metadata. Without it the repos
# are still generated but unsigned, which apt will refuse by default — useful
# for local testing only.
#
# Old versions are deliberately kept. An apt/yum repo that drops previous
# versions breaks pinning, downgrades, and any lockfile referencing them.
set -euo pipefail

pkgdir=${1:?usage: build-repos.sh <packages-dir> <repo-root>}
root=${2:?usage: build-repos.sh <packages-dir> <repo-root>}

apt_root="$root/apt"
yum_root="$root/yum"

# --- APT ---------------------------------------------------------------------
#
# Layout:
#   apt/pool/main/e/exshell/*.deb
#   apt/dists/stable/main/binary-{amd64,arm64}/Packages{,.gz}
#   apt/dists/stable/{Release,InRelease,Release.gpg}
mkdir -p "$apt_root/pool/main/e/exshell"
cp -v "$pkgdir"/*.deb "$apt_root/pool/main/e/exshell/" 2>/dev/null || {
	echo "build-repos: no .deb files in $pkgdir" >&2
	exit 1
}

pushd "$apt_root" >/dev/null

for arch in amd64 arm64; do
	dir="dists/stable/main/binary-$arch"
	mkdir -p "$dir"
	# Paths inside Packages must be relative to the repo root (this dir),
	# which is why dpkg-scanpackages runs from here rather than from pool/.
	dpkg-scanpackages --arch "$arch" pool >"$dir/Packages" 2>/dev/null
	gzip -9cn "$dir/Packages" >"$dir/Packages.gz"
done

# apt-ftparchive computes the checksums over the Packages files that apt then
# verifies against the signed Release.
apt-ftparchive \
	-o APT::FTPArchive::Release::Origin=vanlabeke \
	-o APT::FTPArchive::Release::Label=exshell \
	-o APT::FTPArchive::Release::Suite=stable \
	-o APT::FTPArchive::Release::Codename=stable \
	-o APT::FTPArchive::Release::Architectures="amd64 arm64" \
	-o APT::FTPArchive::Release::Components=main \
	release dists/stable >dists/stable/Release

if [ -n "${GPG_KEY_ID:-}" ]; then
	# InRelease (inline-signed) is what modern apt prefers; Release.gpg is
	# kept for older clients that only look for a detached signature.
	rm -f dists/stable/InRelease dists/stable/Release.gpg
	gpg --batch --yes --default-key "$GPG_KEY_ID" \
		--clearsign -o dists/stable/InRelease dists/stable/Release
	gpg --batch --yes --default-key "$GPG_KEY_ID" \
		-abs -o dists/stable/Release.gpg dists/stable/Release
	gpg --armor --export "$GPG_KEY_ID" >gpg.key
else
	echo "build-repos: GPG_KEY_ID unset — apt repo left UNSIGNED" >&2
fi

popd >/dev/null

# --- YUM ---------------------------------------------------------------------
#
# Layout:
#   yum/{x86_64,aarch64}/*.rpm + repodata/
#   yum/vanlabeke.repo
#
# Note the arch directory names: rpm uses x86_64/aarch64 where Go and dpkg use
# amd64/arm64, so the release filenames do not match the directories.
for pair in "amd64:x86_64" "arm64:aarch64"; do
	goarch=${pair%%:*}
	rpmarch=${pair##*:}
	dir="$yum_root/$rpmarch"
	mkdir -p "$dir"
	# Not a hard failure: a release may legitimately not cover every arch.
	cp -v "$pkgdir"/*"$goarch"*.rpm "$dir/" 2>/dev/null || {
		echo "build-repos: no $goarch .rpm found, skipping $rpmarch" >&2
		continue
	}

	createrepo_c --update "$dir"

	if [ -n "${GPG_KEY_ID:-}" ]; then
		rm -f "$dir/repodata/repomd.xml.asc"
		gpg --batch --yes --default-key "$GPG_KEY_ID" \
			--detach-sign --armor "$dir/repodata/repomd.xml"
	fi
done

if [ -n "${GPG_KEY_ID:-}" ]; then
	gpg --armor --export "$GPG_KEY_ID" >"$yum_root/gpg.key"
fi

# The .repo file users drop into /etc/yum.repos.d/. $basearch is expanded by
# dnf/yum itself, so one file serves both architectures.
cat >"$yum_root/vanlabeke.repo" <<'EOF'
[vanlabeke]
name=vanlabeke packages
baseurl=https://pkg.vanlabeke.dev/yum/$basearch
enabled=1
gpgcheck=1
repo_gpgcheck=1
gpgkey=https://pkg.vanlabeke.dev/yum/gpg.key
EOF

# Pages serves this tree as-is; .nojekyll stops Jekyll from filtering out the
# underscore-prefixed and dotted paths that repo metadata relies on.
touch "$root/.nojekyll"

echo "build-repos: done"
echo "  apt: $apt_root"
echo "  yum: $yum_root"
