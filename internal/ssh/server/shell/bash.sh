# DCX shell integration for bash

# Title integration - only if DCX_PROJECT_NAME is set
if [[ -n "$DCX_PROJECT_NAME" ]]; then
    # Function to set title to directory (called before prompt)
    __dcx_precmd() {
        printf '\e]2;%s@%s:%s\a' "$USER" "$DCX_PROJECT_NAME" "${PWD/#$HOME/\~}"
    }

    # Function to set title to command (called before command execution)
    __dcx_preexec() {
        printf '\e]2;%s@%s: %s\a' "$USER" "$DCX_PROJECT_NAME" "$1"
    }

    # Install precmd via PROMPT_COMMAND
    if [[ -z "$PROMPT_COMMAND" ]]; then
        PROMPT_COMMAND="__dcx_precmd"
    else
        PROMPT_COMMAND="__dcx_precmd;$PROMPT_COMMAND"
    fi

    # Install preexec via DEBUG trap
    __dcx_preexec_invoke() {
        # Only run if we're about to execute a command (not during prompt)
        [[ -n "$COMP_LINE" ]] && return  # Skip during completion
        [[ "$BASH_COMMAND" == "$PROMPT_COMMAND" ]] && return  # Skip prompt command
        __dcx_preexec "$BASH_COMMAND"
    }
    trap '__dcx_preexec_invoke' DEBUG
fi

# Source user's bashrc if it exists
[[ -f ~/.bashrc ]] && source ~/.bashrc
