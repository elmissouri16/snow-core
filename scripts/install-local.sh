#!/bin/sh
set -eu

snow_script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
snow_repo_root=$(dirname -- "$snow_script_dir")
snow_install_dir=${SNOW_INSTALL_DIR:-"${HOME}/.local/bin"}
snow_binary="${snow_install_dir}/snow"

mkdir -p "$snow_install_dir"
snow_staged_binary=$(mktemp "${snow_install_dir}/.snow.XXXXXX")
trap 'rm -f "$snow_staged_binary"' EXIT HUP INT TERM

(
	cd "$snow_repo_root"
	go build -trimpath -ldflags='-s -w' -o "$snow_staged_binary" ./cmd/snow
)

chmod 0755 "$snow_staged_binary"
mv -f "$snow_staged_binary" "$snow_binary"
trap - EXIT HUP INT TERM

printf 'Installed %s\n' "$snow_binary"
"$snow_binary" version

case ":${PATH}:" in
	*":${snow_install_dir}:"*) ;;
	*)
		printf 'Warning: %s is not on PATH. Add it to your shell configuration.\n' "$snow_install_dir" >&2
		;;
esac
