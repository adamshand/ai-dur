ai() {
    local aidur_script="${AIDUR_SCRIPT:-$HOME/.local/bin/aidur.py}"
    command python3 "$aidur_script" "$@"
}
