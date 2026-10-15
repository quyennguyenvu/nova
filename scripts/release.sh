#!/usr/bin/env bash
# Cut a nova release: verify main, run the gates, then push a vX.Y.Z tag.
# The tag push is the release — .github/workflows/release.yaml runs GoReleaser on it.
#
# Usage: scripts/release.sh [--full] patch|minor|major|vX.Y.Z[-prerelease]
set -euo pipefail

usage() {
	echo "usage: $0 [--full] patch|minor|major|vX.Y.Z[-prerelease]" >&2
	echo "  --full  gate on make test-all (~4 min, needs sqlc/wire/buf) instead of make test" >&2
	exit 2
}
die() {
	echo "❌ $*" >&2
	exit 1
}
step() { printf '\n🔹 %s\n' "$*"; }

full=0
if [ "${1:-}" = --full ]; then
	full=1
	shift
fi
[ $# -eq 1 ] || usage

cd "$(git rev-parse --show-toplevel)"

step "Checking the working tree"
[ "$(git rev-parse --abbrev-ref HEAD)" = main ] || die "not on main"
[ -z "$(git status --porcelain)" ] || die "working tree is dirty; commit or stash first"
git fetch --quiet --tags origin main
[ "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)" ] || die "HEAD is not origin/main; pull or push first"

# Bumps start from the latest stable tag, so an rc never becomes the base.
latest=$(git tag --list --sort=-v:refname 'v*' | grep -m1 -E '^v[0-9]+\.[0-9]+\.[0-9]+$' || true)
latest=${latest:-v0.0.0}
IFS=. read -r major minor patch <<<"${latest#v}"

case $1 in
patch) version=v$major.$minor.$((patch + 1)) ;;
minor) version=v$major.$((minor + 1)).0 ;;
major) version=v$((major + 1)).0.0 ;;
v*) version=$1 ;;
*) usage ;;
esac

# No leading zeros: semver forbids them and bash arithmetic would read them as octal.
n='(0|[1-9][0-9]*)'
[[ $version =~ ^v$n\.$n\.$n(-[0-9A-Za-z.-]+)?$ ]] || die "$version is not vMAJOR.MINOR.PATCH[-prerelease]"
vmaj=${BASH_REMATCH[1]} vmin=${BASH_REMATCH[2]} vpat=${BASH_REMATCH[3]}
if ((vmaj < major || (vmaj == major && (vmin < minor || (vmin == minor && vpat <= patch))))); then
	die "$version must be newer than $latest"
fi
if git rev-parse -q --verify "refs/tags/$version" >/dev/null; then
	die "tag $version already exists"
fi

range=HEAD
if [ "$latest" != v0.0.0 ]; then range=$latest..HEAD; fi
[ -n "$(git rev-list "$range")" ] || die "no commits since $latest"

step "Releasing $latest → $version"
step "make lint"
make lint
if ((full)); then
	step "make test-all"
	make test-all
else
	step "make test"
	make test
fi

step "Commits going into $version"
git log --oneline --no-decorate "$range"
case $version in *-*) echo "(pre-release: GitHub will not mark it latest)" ;; esac

printf '\nTag %s at %s and push it to origin? [y/N] ' "$version" "$(git rev-parse --short HEAD)"
read -r answer || true
case $answer in [yY]) ;; *) die "aborted; nothing was tagged" ;; esac

git tag -a "$version" -m "$version"
if ! git push origin "refs/tags/$version"; then
	git tag -d "$version" >/dev/null
	die "push failed; removed the local tag $version"
fi

repo=$(git remote get-url origin | sed -E 's#\.git$##; s#^.*[:/]([^/]+/[^/]+)$#\1#')
printf '\n✅ Pushed %s. GoReleaser is building it:\n' "$version"
echo "   https://github.com/$repo/actions/workflows/release.yaml"
echo "   https://github.com/$repo/releases/tag/$version"
