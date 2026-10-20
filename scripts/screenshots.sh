#!/bin/sh
# Retake the screenshots in docs/images: `make screenshots`, or
# scripts/screenshots.sh [scene...] for some of them.
#
# Starts the devmail harness on a new, empty mailbox in a temporary folder —
# a fake Gmail full of made-up mail, so nothing real can be in a picture —
# drives it with frontend/scripts/screenshots.mjs, and removes it all after.
# Needs Google Chrome installed; it is driven, not downloaded.
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
addr=127.0.0.1:8497
control=127.0.0.1:8498

# Stopping devmail makes `wait` report the signal; that is not a failure of
# the run, so the cleanup does not let it become the exit status.
cleanup() {
	set +e
	if [ -n "${pid:-}" ]; then
		kill "$pid" 2>/dev/null
		wait "$pid" 2>/dev/null
	fi
	rm -rf "$work"
}
trap cleanup EXIT INT TERM

cd "$root"
go build -o "$work/devmail" ./internal/imaptest/devmail
go build -o "$work/iql" ./cmd/iql

# A home of its own: the System section reads the machine settings, the login
# service and the AI tools from the home folder, and a picture of this
# machine's would publish them.
mkdir -p "$work/home"
HOME="$work/home" INBOXQL_HOME="$work/home/.iql" "$work/devmail" -data "$work/data" -addr "$addr" -control "$control" -history 60 >"$work/devmail.log" 2>&1 &
pid=$!

i=0
until curl -fs "http://$addr/" >/dev/null 2>&1; do
	i=$((i + 1))
	if [ "$i" -gt 60 ]; then
		cat "$work/devmail.log" >&2
		echo "screenshots: devmail did not start" >&2
		exit 1
	fi
	sleep 1
done

# Something for Waiting for approval: a reply an agent drafted and queued.
draft=$(HOME="$work/home" "$work/iql" --data "$work/data" --json draft create --account gmail --origin agent \
	--to person2@example.com --subject "Re: Quarterly numbers" \
	--body "Thanks — the figures look right to me. I have moved the review to Thursday." |
	sed -n 's/.*"id": *"\([^"]*\)".*/\1/p' | head -n 1)
HOME="$work/home" "$work/iql" --data "$work/data" --json send "$draft" >/dev/null

status=0
node frontend/scripts/screenshots.mjs "http://$addr" "http://$control" docs/images "$@" || status=$?
exit "$status"
