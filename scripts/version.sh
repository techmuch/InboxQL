#!/bin/sh
# The release version: year.month.day.n, n counting that day's releases.
#
#   scripts/version.sh          print the version in VERSION
#   scripts/version.sh next     write the next one for today and print it
#   scripts/version.sh set V    write V
#
# The version is written in two places — VERSION, which the binary embeds
# and the frontend's build reads, and the literal in internal/cli/serve.go,
# which the linker stamps — and this is what keeps them the same.
# TestVersionMatchesTheVersionFile fails if they ever differ.
#
# "Today" is the local date of the machine making the release: a release made
# at 23:39 belongs to that evening, not to the next morning in UTC.
set -eu
cd "$(dirname "$0")/.."

write() {
	printf '%s\n' "$1" > VERSION
	sed -i.bak -E "s/^var Version = \"[^\"]*\"/var Version = \"$1\"/" internal/cli/serve.go
	rm -f internal/cli/serve.go.bak
	grep -q "^var Version = \"$1\"$" internal/cli/serve.go || {
		echo "could not write the version into internal/cli/serve.go" >&2
		exit 1
	}
}

case "${1:-show}" in
show)
	cat VERSION
	;;
next)
	# No zero-padding: 2026.3.5.1, so each part is a plain number.
	today=$(date +%Y.%m.%d | awk -F. '{ printf "%d.%d.%d", $1, $2, $3 }')
	git fetch --quiet --tags 2>/dev/null || true
	last=$(git tag -l "v$today.*" | sed "s/^v$today\.//" | grep -E '^[0-9]+$' | sort -n | tail -1)
	n=$(( ${last:-0} + 1 ))
	write "$today.$n"
	echo "$today.$n"
	;;
set)
	echo "${2:?set needs a version}" | grep -Eq '^[0-9]{4}\.[0-9]+\.[0-9]+\.[0-9]+$' || {
		echo "a version is year.month.day.n, such as 2026.10.30.2" >&2
		exit 2
	}
	write "$2"
	echo "$2"
	;;
*)
	echo "usage: scripts/version.sh [show | next | set <version>]" >&2
	exit 2
	;;
esac
