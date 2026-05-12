#!/usr/bin/env bash
set -euo pipefail

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
    echo "This helper must run as root." >&2
    exit 1
fi

current_user="${1:-}"
target_user="${2:-}"
target_hostname="${3:-}"

if [[ -z "${current_user}" || -z "${target_user}" ]]; then
    echo "Usage: apply-account-settings.sh <current-user> <target-user> [target-hostname]" >&2
    exit 1
fi

if ! getent passwd "${current_user}" >/dev/null; then
    echo "User ${current_user} does not exist." >&2
    exit 1
fi

if [[ "${target_user}" != "${current_user}" ]] && getent passwd "${target_user}" >/dev/null; then
    echo "User ${target_user} already exists." >&2
    exit 1
fi

if [[ -n "${target_hostname}" ]] && [[ ! "${target_hostname}" =~ ^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$ ]]; then
    echo "Invalid hostname: ${target_hostname}" >&2
    exit 1
fi

password=""
IFS= read -r password || true

final_user="${current_user}"
home_dir="$(getent passwd "${current_user}" | cut -d: -f6)"

if [[ "${target_user}" != "${current_user}" ]]; then
    usermod -l "${target_user}" "${current_user}"
    if getent group "${current_user}" >/dev/null; then
        groupmod -n "${target_user}" "${current_user}" || true
    fi

    if [[ -n "${home_dir}" ]] && [[ "$(basename "${home_dir}")" == "${current_user}" ]]; then
        new_home="$(dirname "${home_dir}")/${target_user}"
        usermod -d "${new_home}" -m "${target_user}"
        home_dir="${new_home}"
    fi

    final_user="${target_user}"
fi

if [[ -n "${password}" ]]; then
    printf '%s:%s\n' "${final_user}" "${password}" | chpasswd
fi

if [[ -n "${target_hostname}" ]]; then
    hostnamectl set-hostname "${target_hostname}"
fi

if [[ -n "${home_dir}" ]] && [[ -d "${home_dir}" ]]; then
    chown -R "${final_user}:${final_user}" "${home_dir}" || true
fi
