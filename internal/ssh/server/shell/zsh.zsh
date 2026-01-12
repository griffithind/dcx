# DCX shell integration for zsh

# Title integration - only if DCX_PROJECT_NAME is set
if [[ -n "$DCX_PROJECT_NAME" ]]; then
    __dcx_precmd() {
        print -Pn "\e]2;${USER}@${DCX_PROJECT_NAME}:%~\a"
    }
    __dcx_preexec() {
        # ${(V)1} converts control characters to visible form
        print -Pn "\e]2;${USER}@${DCX_PROJECT_NAME}: ${(V)1}\a"
    }
    autoload -Uz add-zsh-hook
    add-zsh-hook precmd __dcx_precmd
    add-zsh-hook preexec __dcx_preexec
fi
