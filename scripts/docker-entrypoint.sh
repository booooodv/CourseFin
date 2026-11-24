#!/bin/sh
set -eu

IMAGE_BIN="${COURSEFIN_IMAGE_BIN:-/usr/local/bin/coursefin}"
DATA_DIR="${COURSEFIN_DATA_DIR:-/data}"
BIN_DIR="${DATA_DIR}/updates/bin"
ACTIVE_BIN="${BIN_DIR}/coursefin"
BACKUP_BIN="${BIN_DIR}/coursefin.bak"
PENDING_FILE="${BIN_DIR}/.pending_update"

# probe_version BIN prints normalized SemVer on stdout and returns 0 if BIN
# is an executable regular file that responds to --version with a valid SemVer.
probe_version() {
	bin_path="$1"
	if [ ! -f "$bin_path" ] || [ ! -x "$bin_path" ]; then
		return 1
	fi
	if ! raw_ver=$("$bin_path" --version 2>/dev/null); then
		return 1
	fi
	printf '%s\n' "$raw_ver" | awk '
		NR > 1 { bad = 1; exit 1 }
		{
			gsub(/^[ \t\r\n]+|[ \t\r\n]+$/, "", $0)
			if ($0 ~ /^[vV]?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?([+][0-9A-Za-z.-]+)?$/) {
				sub(/^[vV]/, "", $0)
				print $0
				ok = 1
			} else {
				bad = 1
				exit 1
			}
		}
		END { if (bad || !ok) exit 1 }
	'
}

# semver_gt A B returns 0 if numeric major.minor.patch of A > B
# (ignoring leading v and prerelease/build tags), 1 otherwise.
semver_gt() {
	awk -v a="$1" -v b="$2" '
		function parse(s, arr,   n, i) {
			sub(/^[vV]/, "", s)
			sub(/[-+].*$/, "", s)
			n = split(s, arr, ".")
			if (n != 3) return 0
			for (i = 1; i <= 3; i++) {
				if (arr[i] !~ /^(0|[1-9][0-9]*)$/) return 0
			}
			return 1
		}
		BEGIN {
			if (!parse(a, pa) || !parse(b, pb)) exit 1
			for (i = 1; i <= 3; i++) {
				va = pa[i] + 0
				vb = pb[i] + 0
				if (va > vb) exit 0
				if (va < vb) exit 1
			}
			exit 1
		}
	'
}

if [ -f "$PENDING_FILE" ]; then
	pending_state=$(tr -d '[:space:]' < "$PENDING_FILE" 2>/dev/null || true)
	if [ "$pending_state" = "tried" ]; then
		echo "coursefin-entrypoint: previous update failed before startup; rolling back" >&2
		if probe_version "$BACKUP_BIN" >/dev/null 2>&1; then
			mv -f "$BACKUP_BIN" "$ACTIVE_BIN"
		else
			rm -f "$ACTIVE_BIN"
		fi
		rm -f "$PENDING_FILE"
	else
		printf 'tried\n' > "$PENDING_FILE"
	fi
fi

IMG_VER=$(probe_version "$IMAGE_BIN" || true)

if [ -e "$ACTIVE_BIN" ] || [ -L "$ACTIVE_BIN" ]; then
	if ACT_VER=$(probe_version "$ACTIVE_BIN"); then
		if [ -n "$IMG_VER" ] && semver_gt "$ACT_VER" "$IMG_VER"; then
			export COURSEFIN_MANAGED_BIN_DIR="$BIN_DIR"
			exec "$ACTIVE_BIN" "$@"
		fi
	else
		echo "coursefin-entrypoint: active binary is unusable; checking backup" >&2
		if BAK_VER=$(probe_version "$BACKUP_BIN"); then
			if [ -n "$IMG_VER" ] && semver_gt "$BAK_VER" "$IMG_VER"; then
				mv -f "$BACKUP_BIN" "$ACTIVE_BIN"
				export COURSEFIN_MANAGED_BIN_DIR="$BIN_DIR"
				exec "$ACTIVE_BIN" "$@"
			fi
		fi
		rm -f "$ACTIVE_BIN"
	fi
fi

export COURSEFIN_MANAGED_BIN_DIR="$BIN_DIR"
exec "$IMAGE_BIN" "$@"
