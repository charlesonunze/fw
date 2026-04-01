#!/usr/bin/env bash
## Check if goimports-reviser is installed
if ! tool_loc="$(type -p goimports-reviser)" || [[ -z ${tool_loc} ]]; then
    echo "goimports-reviser is not installed. installing...."
    go install -v github.com/incu6us/goimports-reviser/v3@latest
fi

# Use full path, if we need it
GOGROUP_PATH="$(type -p goimports-reviser)"
if [[ -z "$GOGROUP_PATH" ]]; then
    echo "goimports-reviser is still not found. Please check installation."
    exit 1
fi

goimports-reviser -rm-unused -set-alias -excludes './cmd,./vendor,./.git' -company-prefixes 'fw' -imports-order 'std,project,company,general,blanked,dotted' -format ./...
