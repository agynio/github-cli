#!/bin/bash

# Arrays of issue URLs and their descriptions
issue_urls=(
    "https://github.com/cli/cli/issues/11114" # (spam) legible nonsense
    "https://github.com/cli/cli/issues/11230" # (spam) Only title no body
    "https://github.com/cli/cli/issues/11222" # (spam) quoting a user reply, unrendered markdown
    "https://github.com/cli/cli/issues/11236" # (spam) a template copy
    "https://github.com/cli/cli/issues/11223" # (spam) a link
    "https://github.com/cli/cli/issues/11242" # (spam) two words
    "https://github.com/cli/cli/issues/11272" # (not spam) short
    "https://github.com/cli/cli/issues/11239" # (not spam) misc oss community
    "https://github.com/cli/cli/issues/11129" # (not spam) filled out bug template
    "https://github.com/cli/cli/issues/11241" # (not spam) filled out feature request template
    "https://github.com/cli/cli/issues/11209" # (not spam) no template
    "https://github.com/cli/cli/issues/11157" # (not spam) misc oss community
    "https://github.com/cli/cli/issues/10448" # (not spam) misc oss community
)

issue_descriptions=(
    "(spam) legible nonsense"
    "(spam) Only title no body"
    "(spam) quoting a user reply, unrendered markdown"
    "(spam) a template copy"
    "(spam) a link"
    "(spam) two words"
    "(not spam) short"
    "(not spam) misc oss community"
    "(not spam) filled out bug template"
    "(not spam) filled out feature request template"
    "(not spam) no template"
    "(not spam) misc oss community"
    "(not spam) misc oss community"
)

models=(
    # "openai/gpt-4.1"
    # "openai/gpt-4.1-mini"
    "openai/gpt-4o"
    # "openai/gpt-4o-mini"
    "xai/grok-3"
    # "xai/grok-3-mini"
)

for model in "${!models[@]}"; do
    echo "Running test suite with ${models[$model]}."
    echo "FAIL = spam, PASS = not spam"
    echo "==================================================="
    echo

    for i in "${!issue_urls[@]}"; do
        # echo "Testing: ${issue_urls[$i]} ${issue_descriptions[$i]}"
        output=$(./.github/workflows/scripts/detect-spam.sh "${issue_urls[$i]}" "${models[$model]}")

        overall_result=$(echo "$output" | grep -e "Overall Result" | cut -d ':' -f2)

        # Print the name of the test and the overall result
        printf "%-45s %-55s %s\n" "${issue_descriptions[$i]}" "${issue_urls[$i]}" "$overall_result"
    done

    echo 

done