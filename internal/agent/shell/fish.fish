# DCX shell integration for fish
if set -q DCX_PROJECT_NAME
    function fish_title
        if test -n "$argv[1]"
            echo "$DCX_PROJECT_NAME: $argv[1]"
        else
            echo "$DCX_PROJECT_NAME:"(prompt_pwd)
        end
    end
end
