function ai
    set -l aidur_script "$HOME/.local/bin/aidur.py"
    if set -q AIDUR_SCRIPT
        set aidur_script "$AIDUR_SCRIPT"
    end
    command python3 $aidur_script $argv
end
