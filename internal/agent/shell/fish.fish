# DCX shell integration for fish
if set -q DCX_TITLE
    function fish_title
        if test -n "$argv[1]"
            echo "$DCX_TITLE: $argv[1]"
        else
            echo "$DCX_TITLE:"(prompt_pwd)
        end
    end
end
