#!/bin/sh
set -eu

snow_repository=elmissouri16/snow-core
snow_install_dir=${SNOW_INSTALL_DIR:-"${HOME:?HOME must be set}/.local/bin"}
snow_requested_version=${SNOW_VERSION:-}
snow_no_modify_path=${SNOW_NO_MODIFY_PATH:-0}
snow_staged_binary=

snow_fail() {
	printf 'snow installer: %s\n' "$*" >&2
	exit 1
}

for snow_command in curl tar uname mktemp mkdir chmod mv cp rm sed awk grep wc; do
	command -v "$snow_command" >/dev/null 2>&1 || snow_fail "required command not found: $snow_command"
done

case "$snow_no_modify_path" in
	0 | 1) ;;
	*) snow_fail "SNOW_NO_MODIFY_PATH must be 0 or 1" ;;
esac
case "$snow_install_dir" in
	/*) ;;
	*) snow_fail "install directory must be an absolute path" ;;
esac
case "$snow_install_dir" in
	*:*) snow_fail "install directory must not contain a colon" ;;
esac
if printf '%s' "$snow_install_dir" | LC_ALL=C grep -q '[[:cntrl:]]'; then
	snow_fail "install directory must not contain control characters"
fi

case "$(uname -s)" in
	Linux) snow_os=linux ;;
	Darwin) snow_os=darwin ;;
	*) snow_fail "unsupported operating system: $(uname -s)" ;;
esac

case "$(uname -m)" in
	x86_64 | amd64) snow_arch=amd64 ;;
	aarch64 | arm64) snow_arch=arm64 ;;
	*) snow_fail "unsupported architecture: $(uname -m)" ;;
esac

snow_temp_dir=$(mktemp -d "${TMPDIR:-/tmp}/snow-install.XXXXXX")
snow_cleanup() {
	if [ -n "$snow_staged_binary" ]; then
		rm -f "$snow_staged_binary"
	fi
	rm -rf "$snow_temp_dir"
}
trap snow_cleanup EXIT
trap 'exit 1' HUP INT TERM

snow_download() {
	snow_url=$1
	snow_destination=$2
	snow_max_bytes=$3
	curl --proto '=https' --tlsv1.2 --fail --silent --show-error --location \
		--retry 3 --max-time 120 --max-filesize "$snow_max_bytes" \
		--output "$snow_destination" "$snow_url"
	snow_downloaded_bytes=$(wc -c <"$snow_destination")
	[ "$snow_downloaded_bytes" -le "$snow_max_bytes" ] || \
		snow_fail "download exceeds ${snow_max_bytes}-byte limit: $snow_url"
}

snow_shell_quote() {
	printf "'"
	printf '%s' "$1" | sed "s/'/'\\\\''/g"
	printf "'"
}

snow_path_exists() {
	[ -e "$1" ] || [ -L "$1" ]
}

snow_configure_path() {
	if [ "$snow_no_modify_path" = 1 ]; then
		printf 'Skipped shell PATH update because SNOW_NO_MODIFY_PATH=1.\n'
		return
	fi

	snow_home=${HOME:-}
	case "$snow_home" in
		/*) ;;
		*)
			printf 'Could not update PATH: HOME is not an absolute path.\n' >&2
			return
			;;
	esac
	if printf '%s' "$snow_home" | LC_ALL=C grep -q '[[:cntrl:]]'; then
		printf 'Could not update PATH: HOME contains control characters.\n' >&2
		return
	fi

	case "${SHELL:-}" in
		*/zsh | zsh)
			snow_zdotdir=${ZDOTDIR:-$snow_home}
			case "$snow_zdotdir" in
				/*) ;;
				*)
					printf 'Could not update PATH: ZDOTDIR is not an absolute path.\n' >&2
					return
					;;
			esac
			if printf '%s' "$snow_zdotdir" | LC_ALL=C grep -q '[[:cntrl:]]'; then
				printf 'Could not update PATH: ZDOTDIR contains control characters.\n' >&2
				return
			fi
			snow_shell_profile="$snow_zdotdir/.zshrc"
			;;
		*/bash | bash)
			if [ "$snow_os" = darwin ]; then
				if snow_path_exists "$snow_home/.bash_profile"; then
					snow_shell_profile="$snow_home/.bash_profile"
				elif snow_path_exists "$snow_home/.bash_login"; then
					snow_shell_profile="$snow_home/.bash_login"
				elif snow_path_exists "$snow_home/.profile"; then
					snow_shell_profile="$snow_home/.profile"
				else
					snow_shell_profile="$snow_home/.bash_profile"
				fi
			else
				snow_shell_profile="$snow_home/.bashrc"
			fi
			;;
		*) snow_shell_profile="$snow_home/.profile" ;;
	esac

	if snow_path_exists "$snow_shell_profile" && \
		[ ! -f "$snow_shell_profile" ]; then
		printf 'Could not update PATH: %s is not a regular file.\n' \
			"$snow_shell_profile" >&2
		return
	fi

	snow_quoted_install_dir=$(snow_shell_quote "$snow_install_dir")
	snow_path_line="export PATH=${snow_quoted_install_dir}:\"\$PATH\""
	if [ -f "$snow_shell_profile" ] && \
		grep -Fqx "$snow_path_line" "$snow_shell_profile"; then
		printf 'Snow PATH is already configured in %s.\n' "$snow_shell_profile"
		return
	fi

	if ! {
		[ ! -s "$snow_shell_profile" ] || printf '\n'
		printf '%s\n' '# Added by the Snow installer.' "$snow_path_line"
	} >>"$snow_shell_profile"; then
		printf 'Could not update PATH in %s.\n' "$snow_shell_profile" >&2
		return
	fi
	printf 'Added %s to PATH in %s. Restart your shell to use snow.\n' \
		"$snow_install_dir" "$snow_shell_profile"
}

if [ -n "$snow_requested_version" ]; then
	case "$snow_requested_version" in
		v*) snow_tag=$snow_requested_version ;;
		*) snow_tag=v$snow_requested_version ;;
	esac
else
	snow_releases_json="$snow_temp_dir/releases.json"
	snow_download \
		"https://api.github.com/repos/${snow_repository}/releases?per_page=1" \
		"$snow_releases_json" 1048576
	snow_tag=$(sed -n 's/^[[:space:]]*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*$/\1/p' "$snow_releases_json" | sed -n '1p')
	[ -n "$snow_tag" ] || snow_fail "no published GitHub release found"
fi

case "$snow_tag" in
	v[0-9]* ) ;;
	*) snow_fail "invalid release version: $snow_tag" ;;
esac
case "$snow_tag" in
	*[!0-9A-Za-z._+-]*) snow_fail "invalid release version: $snow_tag" ;;
esac

snow_version=${snow_tag#v}
snow_archive="snow_${snow_version}_${snow_os}_${snow_arch}.tar.gz"
snow_release_url="https://github.com/${snow_repository}/releases/download/${snow_tag}"
snow_archive_path="$snow_temp_dir/$snow_archive"
snow_checksums_path="$snow_temp_dir/SHA256SUMS"

printf 'Downloading Snow %s for %s/%s...\n' "$snow_version" "$snow_os" "$snow_arch"
snow_download "$snow_release_url/$snow_archive" "$snow_archive_path" 134217728
snow_download "$snow_release_url/SHA256SUMS" "$snow_checksums_path" 65536

snow_expected_checksum=$(awk -v archive="$snow_archive" '$2 == archive { print $1; exit }' "$snow_checksums_path")
printf '%s\n' "$snow_expected_checksum" | grep -Eq '^[0-9a-fA-F]{64}$' || \
	snow_fail "release checksum is missing or malformed for $snow_archive"

if command -v sha256sum >/dev/null 2>&1; then
	snow_actual_checksum=$(sha256sum "$snow_archive_path" | awk '{ print $1 }')
elif command -v shasum >/dev/null 2>&1; then
	snow_actual_checksum=$(shasum -a 256 "$snow_archive_path" | awk '{ print $1 }')
else
	snow_fail "SHA-256 verification requires sha256sum or shasum"
fi

[ "$snow_actual_checksum" = "$snow_expected_checksum" ] || \
	snow_fail "checksum verification failed for $snow_archive"

snow_archive_root="snow_${snow_version}_${snow_os}_${snow_arch}"
snow_members_path="$snow_temp_dir/archive-members"
snow_verbose_members_path="$snow_temp_dir/archive-members-verbose"
LC_ALL=C tar -tzf "$snow_archive_path" >"$snow_members_path" || \
	snow_fail "cannot list release archive"
awk -v root="$snow_archive_root" '
	$0 == root "/" { directories++; next }
	$0 == root "/LICENSE" { licenses++; next }
	$0 == root "/README.md" { readmes++; next }
	$0 == root "/snow" { binaries++; next }
	{ unexpected++ }
	END {
		if (directories != 1 || licenses != 1 || readmes != 1 || binaries != 1 || unexpected != 0) {
			exit 1
		}
	}
' "$snow_members_path" || snow_fail "release archive contains unexpected paths"
LC_ALL=C tar -tvzf "$snow_archive_path" >"$snow_verbose_members_path" || \
	snow_fail "cannot inspect release archive members"
awk -v root="$snow_archive_root" '
	function member_size(    i) {
		for (i = 2; i < NF; i++) {
			if ($i ~ /^[0-9]+$/ && ($(i + 1) ~ /^[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]$/ || $(i + 1) ~ /^(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)$/)) {
				return $i + 0
			}
		}
		return -1
	}
	{
		size = member_size()
		name = $NF
		type = substr($1, 1, 1)
		if (name == root "/" && type == "d" && size >= 0 && size <= 4096) {
			directories++; total += size; next
		}
		if (name == root "/LICENSE" && type == "-" && size >= 0 && size <= 1048576) {
			licenses++; total += size; next
		}
		if (name == root "/README.md" && type == "-" && size >= 0 && size <= 4194304) {
			readmes++; total += size; next
		}
		if (name == root "/snow" && type == "-" && size >= 0 && size <= 134217728) {
			binaries++; total += size; next
		}
		unexpected++
	}
	END {
		if (directories != 1 || licenses != 1 || readmes != 1 || binaries != 1 || unexpected != 0 || total > 139464704) {
			exit 1
		}
	}
' "$snow_verbose_members_path" || snow_fail "release archive contains unsafe member types or sizes"

snow_extract_dir="$snow_temp_dir/extract"
mkdir -p "$snow_extract_dir"
(
	ulimit -f 262144
	tar -xzf "$snow_archive_path" -C "$snow_extract_dir"
) || snow_fail "release archive expansion exceeded its safety limit"
snow_extracted_root="$snow_extract_dir/$snow_archive_root"
snow_extracted_binary="$snow_extracted_root/snow"
[ -d "$snow_extracted_root" ] && [ ! -L "$snow_extracted_root" ] || \
	snow_fail "release archive does not contain a safe root directory"
[ -f "$snow_extracted_root/LICENSE" ] && [ ! -L "$snow_extracted_root/LICENSE" ] && \
	[ -f "$snow_extracted_root/README.md" ] && [ ! -L "$snow_extracted_root/README.md" ] && \
	[ -f "$snow_extracted_binary" ] && [ ! -L "$snow_extracted_binary" ] || \
	snow_fail "release archive does not contain safe regular files"
snow_extracted_total=0
for snow_extracted_file in "$snow_extracted_root/LICENSE" "$snow_extracted_root/README.md" "$snow_extracted_binary"; do
	snow_extracted_size=$(wc -c <"$snow_extracted_file")
	snow_extracted_total=$((snow_extracted_total + snow_extracted_size))
done
[ "$snow_extracted_total" -le 139460608 ] || snow_fail "expanded release archive exceeds its total size limit"
chmod 0755 "$snow_extracted_binary"

snow_reported_version=$("$snow_extracted_binary" version) || \
	snow_fail "downloaded snow binary failed its version check"
[ "$snow_reported_version" = "$snow_version" ] || \
	snow_fail "downloaded binary reports $snow_reported_version, expected $snow_version"

mkdir -p "$snow_install_dir"
snow_destination="$snow_install_dir/snow"
if [ -e "$snow_destination" ] || [ -L "$snow_destination" ]; then
	[ -f "$snow_destination" ] && [ ! -L "$snow_destination" ] || \
		snow_fail "install destination exists and is not a regular file: $snow_destination"
fi
snow_staged_binary=$(mktemp "$snow_install_dir/.snow.XXXXXX")
cp "$snow_extracted_binary" "$snow_staged_binary"
chmod 0755 "$snow_staged_binary"
mv -f "$snow_staged_binary" "$snow_destination"
snow_staged_binary=

printf 'Installed Snow %s to %s/snow\n' "$snow_version" "$snow_install_dir"
snow_configure_path
