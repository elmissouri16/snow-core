#!/bin/sh
set -eu

pages_script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
pages_repo_root=$(dirname -- "$pages_script_dir")
pages_output=${1:-"$pages_repo_root/_pages_source"}

pages_fail() {
	printf 'pages builder: %s\n' "$*" >&2
	exit 1
}

for pages_command in git find mkdir cp cat sed mktemp dirname; do
	command -v "$pages_command" >/dev/null 2>&1 || pages_fail "required command not found: $pages_command"
done

[ ! -e "$pages_output" ] || pages_fail "output already exists: $pages_output"
mkdir -p "$pages_output"

pages_copy_file_as() {
	pages_file=$1
	pages_target=$2
	pages_source="$pages_repo_root/$pages_file"
	[ -f "$pages_source" ] && [ ! -L "$pages_source" ] || \
		pages_fail "allowlisted source is not a regular file: $pages_file"
	pages_destination="$pages_output/$pages_target"
	mkdir -p "$(dirname -- "$pages_destination")"
	cp "$pages_source" "$pages_destination"
}

pages_copy_file() {
	pages_copy_file_as "$1" "$1"
}

pages_copy_tracked() {
	git -C "$pages_repo_root" ls-files -- "$1" | while IFS= read -r pages_file; do
		[ -n "$pages_file" ] || continue
		pages_copy_file "$pages_file"
	done
}

for pages_site_file in \
	site/_config.yml \
	site/_includes/navigation.html \
	site/_layouts/default.html \
	site/_layouts/home.html \
	site/assets/css/style.css \
	site/index.md \
	site/404.html \
	site/examples/index.md \
	site/examples/sdk/index.md \
	site/pkg/snowsdk/index.md \
	site/pkg/protocol/schema/rpc/v1/index.md
do
	pages_site_destination=${pages_site_file#site/}
	pages_copy_file_as "$pages_site_file" "$pages_site_destination"
done

for pages_public_document in \
	docs/getting-started.md \
	docs/using-snow.md \
	docs/configuration.md \
	docs/chatgpt-auth.md \
	docs/sessions.md \
	docs/plan-mode.md \
	docs/goals.md \
	docs/subagents.md \
	docs/skills.md \
	docs/mcp.md \
	docs/plugins.md \
	docs/security.md \
	docs/sdk.md \
	docs/rpc.md \
	docs/user-input.md \
	docs/plugin-protocol.md
do
	pages_copy_file "$pages_public_document"
done

pages_copy_tracked examples/sdk
pages_copy_tracked pkg/protocol/schema/rpc/v1

find "$pages_output" -type f -name '*.md' -exec sh -c '
	set -eu
	for pages_markdown do
		if [ "$(sed -n "1p" "$pages_markdown")" = "---" ]; then
			continue
		fi
		pages_title=$(sed -n "s/^# //p" "$pages_markdown" | sed -n "1p")
		[ -n "$pages_title" ] || pages_title=Documentation
		pages_title=$(printf "%s" "$pages_title" | sed "s/\\\\/\\\\\\\\/g; s/\"/\\\\\"/g")
		pages_staged=$(mktemp "${pages_markdown}.XXXXXX")
		{
			printf "%s\n" "---" "layout: default" "title: \"$pages_title\"" "---" ""
			cat "$pages_markdown"
		} >"$pages_staged"
		mv -f "$pages_staged" "$pages_markdown"
	done
' sh {} +

printf 'Prepared GitHub Pages source in %s\n' "$pages_output"
