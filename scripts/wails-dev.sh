#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_dir="$(cd "${script_dir}/.." && pwd)"

tags=()
if command -v pkg-config >/dev/null 2>&1; then
    if pkg-config --exists webkit2gtk-4.1; then
        tags+=("webkit2_41")
    fi
fi

extra_tags="${CARACAL_WAILS_EXTRA_TAGS:-}"
if [[ -n "${extra_tags}" ]]; then
    IFS=',' read -r -a requested <<< "${extra_tags}"
    for tag in "${requested[@]}"; do
        trimmed="$(printf '%s' "${tag}" | xargs)"
        if [[ -n "${trimmed}" ]]; then
            tags+=("${trimmed}")
        fi
    done
fi

cd "${repo_dir}"

if [[ ${#tags[@]} -gt 0 ]]; then
    joined_tags="$(IFS=,; printf '%s' "${tags[*]}")"
    exec wails dev -tags "${joined_tags}" "$@"
fi

exec wails dev "$@"
