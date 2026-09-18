#!/usr/bin/env bash
# Probe news feed URLs and report reachability, format, and caching support.
# Read-only: makes one HEAD-like GET per URL and writes nothing to the repo.
set -euo pipefail

UA="tailscale-news-healthcheck/1.0 (+https://github.com/tailscale-news)"
TIMEOUT="${FEED_TIMEOUT:-15}"
MAX_BYTES="${FEED_MAX_BYTES:-1048576}"

usage() {
	cat <<'EOF'
Usage: check-feeds.sh <url>... | check-feeds.sh -f <file-with-one-url-per-line>

Environment:
  FEED_TIMEOUT     per-request timeout in seconds (default 15)
  FEED_MAX_BYTES   maximum body bytes downloaded per feed (default 1048576)
EOF
}

urls=()
if [[ $# -eq 0 ]]; then
	usage
	exit 2
elif [[ "$1" == "-f" ]]; then
	[[ $# -eq 2 ]] || { usage; exit 2; }
	[[ -r "$2" ]] || { echo "cannot read $2" >&2; exit 2; }
	while IFS= read -r line; do
		line="${line%%#*}"
		line="$(printf '%s' "$line" | tr -d '[:space:]')"
		[[ -n "$line" ]] && urls+=("$line")
	done <"$2"
else
	urls=("$@")
fi

command -v curl >/dev/null || { echo "curl is required" >&2; exit 1; }

probe() {
	local url="$1" body meta code ctype etag lastmod final size verdict detail

	case "$url" in
	https://* | http://*) ;;
	*)
		printf '%-9s %s\n\t\treason: unsupported scheme\n' "BROKEN" "$url"
		return
		;;
	esac

	body="$(mktemp)"
	trap 'rm -f "$body"' RETURN

	if ! meta="$(curl --silent --show-error --location --max-redirs 5 \
		--max-time "$TIMEOUT" --max-filesize "$MAX_BYTES" \
		--user-agent "$UA" --proto '=http,https' \
		--output "$body" \
		--write-out '%{http_code}\t%{content_type}\t%{size_download}\t%{url_effective}' \
		"$url" 2>&1)"; then
		printf '%-9s %s\n\t\treason: %s\n' "BROKEN" "$url" "${meta//$'\n'/ }"
		return
	fi

	IFS=$'\t' read -r code ctype size final <<<"$meta"

	# Second lightweight request only for caching headers.
	etag="$(curl --silent --head --location --max-time "$TIMEOUT" --user-agent "$UA" "$url" |
		awk 'BEGIN{IGNORECASE=1} /^etag:/{sub(/^[^:]*: */,""); print; exit}' | tr -d '\r')"
	lastmod="$(curl --silent --head --location --max-time "$TIMEOUT" --user-agent "$UA" "$url" |
		awk 'BEGIN{IGNORECASE=1} /^last-modified:/{sub(/^[^:]*: */,""); print; exit}' | tr -d '\r')"

	detail=""
	if [[ "$code" =~ ^2 ]]; then
		if grep -qiE '<(rss|feed|rdf:RDF)\b' "$body" || [[ "$ctype" == *json* ]]; then
			verdict="OK"
		else
			verdict="DEGRADED"
			detail="body is not RSS/Atom/JSON — HTML scrape parser required"
		fi
		if [[ -z "$etag" && -z "$lastmod" ]]; then
			detail="${detail:+$detail; }no ETag or Last-Modified — conditional polling unavailable"
			[[ "$verdict" == "OK" ]] && verdict="DEGRADED"
		fi
	else
		verdict="BROKEN"
		detail="HTTP $code"
	fi

	if [[ "$final" != "$url" ]]; then
		[[ "$verdict" == "OK" ]] && verdict="MOVED"
		detail="${detail:+$detail; }redirected to $final"
	fi

	printf '%-9s %s\n' "$verdict" "$url"
	printf '\t\thttp=%s type=%s bytes=%s\n' "$code" "${ctype:-unknown}" "$size"
	printf '\t\tetag=%s last-modified=%s\n' "${etag:-none}" "${lastmod:-none}"
	[[ -n "$detail" ]] && printf '\t\tnote: %s\n' "$detail"
}

exit_code=0
for u in "${urls[@]}"; do
	probe "$u"
done
exit "$exit_code"
