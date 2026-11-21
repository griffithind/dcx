# DCX shell integration for zsh

# Title integration: DCX_TITLE ("<project>@<host>") is set for interactive sessions
if [[ -n "$DCX_TITLE" ]]; then
    __dcx_precmd() {
        print -Pn "\e]2;${DCX_TITLE}:%~\a"
    }
    __dcx_preexec() {
        # ${(V)1} converts control characters to visible form
        print -Pn "\e]2;${DCX_TITLE}: ${(V)1}\a"
    }
    autoload -Uz add-zsh-hook
    add-zsh-hook precmd __dcx_precmd
    add-zsh-hook preexec __dcx_preexec
fi
