#!/bin/bash

_issue_url="$1"

_issue_body="$(gh issue view $_issue_url --json body -q '.body')"
_issue_title="$(gh issue view $_issue_url --json title -q '.title')"

# _issue_body="gh issue create"

_prompt="
<ISSUE TITLE:>
$_issue_title
</ISSUE TITLE:>

<ISSUE BODY:>
$_issue_body
</ISSUE BODY:>
"

_system_prompt='
# Your role as a spam detection system

You are a spam detection AI. You determine if the provided GitHub issue is spam or not.

## Response format

Respond with a JSON object according to the below schema:

```
{
    "template_similarity": {
        "template_match_score": {
            "description": "A number describing how similar the issue is to the provided project templates.",
            "definition of 0": "The issue does not match any of the provided project templates.",
            "definition of 5": "The issue has some similarities like headings or comments, but other content has been added significantly by the author.",
            "definition of 10": "The issue matches the provided project template EXACTLY, verbatim, and has no user additions or edits. Not even a SINGLE CHARACTER is different.",
            "value": 0
        },
        "user_edits_score": {
            "description": "A number describing how much the user has edited the issue after creating it.",
            "definition of 0": "The issue has been edited significantly. No headings or comments match a template.",
            "definition of 5": "The issue has been edited by the user, but still contains some template-like headings or comments.",
            "definition of 10": "The issue has not been edited by the user at all. It matches a template exactly.",
            "value": 0
        }
    },
    "github_relatedness": {
        "github_unrelated_score": {
            "description": "A number describing how related the issue content is to GitHub.",
            "definition of 0": "The issue is completely related to GitHub.",
            "definition of 5": "The issue has some relation to GitHub, but also includes unrelated content.",
            "definition of 10": "The issue is completely unrelated to GitHub.",
            "value": 0
        },
        "cli_unrelated_score": {
            "description": "A number describing how related the issue content is to CLI tools.",
            "definition of 0": "The issue is completely related to CLI tools.",
            "definition of 5": "The issue has some relation to CLI tools, but also includes unrelated content.",
            "definition of 10": "The issue is completely unrelated to CLI tools.",
            "value": 0
        }
    },
    "content_quality": {
        "nonsense_score": {
            "description": "A number describing how sensible and grounded the issue content is.",
            "definition of 0": "The issue is completely sensible and grounded.",
            "definition of 5": "The issue has some elements that could be confusing, but is still completely grounded.",
            "definition of 10": "The issue is largely nonsense and largely not grounded.",
            "value": 0
        },
        "logical_continuity_score": {
            "description": "A number describing how logically the issue content flows.",
            "definition of 0": "Sentences flow logically and coherently without grammatical errors.",
            "definition of 5": "Sentences flow logically but has some grammatical errors or awkward phrasing.",
            "definition of 10": "Sentences largely do not flow logically.",
            "value": 0
        },
        "effort_score": {
            "description": "A number describing the effort put into writing the issue.",
            "definition of 0": "The issue shows significant effort and thought put into it, like a detailed description or multiple paragraphs.",
            "definition of 5": "The issue shows some effort, like a couple sentences.",
            "definition of 10": "The issue shows no effort at all, like a single word or a link.",
            "value": 0
        },
        "title_logical_association_with_body_score": {
            "description": "A number describing how logically the issue title is associated with the body content.",
            "definition of 0": "The title is completely logically associated with the body content.",
            "definition of 5": "The title has some association with the body content, but is not directly related.",
            "definition of 10": "The title is completely unrelated to the body content, in a different language, or either is empty.",
            "value": 0
        }
    }
}
```

## Project context

Issues related to the GitHub CLI tool are less likely to be spam.

This is the GitHub CLI (gh) project - a command-line tool for GitHub. Legitimate issues should be related to:

- Bug reports about the CLI tool functionality
- Feature requests for new CLI commands or improvements
- Documentation issues
- Installation or usage problems
- Questions about CLI behavior
- Sometimes GitHub-related issues that are relevant to the CLI context

## Issue templates

Issues that exactly match the issue templates defined below are likely spam.

Here are the issue templates already defined in the project:

'

# Append the issue templates to the system prompt.
_template_index=1
for template_file in .github/ISSUE_TEMPLATE/*.md; do
    if ! [[ -f "$template_file" ]]; then
        continue
    fi

    _template_content="$(cat "$template_file")"

    # Remove YAML front matter (everything between the first two --- lines)
    _template_content="$(echo "$_template_content" | sed '1,/^---$/d; /^---$/,$d')"
    _escaped_template="${_template_content//\`/\\\`}"
    
    _system_prompt="${_system_prompt}

<Template ${_template_index}>

\`\`\`
${_escaped_template}
\`\`\`
</Template ${_template_index}>
"

    ((_template_index++))
done

_request_body_tmpl='
    {
        "response_format": {
            "type": "json_object"
        },
        "messages": [
            {
                "role": "system",
                "content": ""
            },
            {
                "role": "user",
                "content": ""
            }
        ],
        "model": ""
    }
'

_model="$2"
if [[ -z "$_model" ]]; then
    _model="openai/gpt-4o-mini"
fi

_request_body="$(jq --arg content "$_prompt" --arg system "$_system_prompt" --arg model "$_model" '.messages[0].content = $system | .messages[1].content = $content | .model = $model' <<< "$_request_body_tmpl")"

_resp="$(curl --silent -L \
  -X POST \
  -H "Accept: application/vnd.github+json" \
  -H "Authorization: Bearer $(gh auth token)" \
  -H "X-GitHub-Api-Version: 2022-11-28" \
  -H "Content-Type: application/json" \
  https://models.github.ai/inference/chat/completions \
  -d "$_request_body"
  )"

# Parse the raw response content
_result="$(jq -r '.choices[0].message.content' <<< "$_resp")"

# Parse individual fields from the JSON response
_template_match_score="$(jq -r '.template_similarity.template_match_score.value' <<< "$_result")"
_user_edits_score="$(jq -r '.template_similarity.user_edits_score.value' <<< "$_result")"
_github_unrelated_score="$(jq -r '.github_relatedness.github_unrelated_score.value' <<< "$_result")"
_cli_unrelated_score="$(jq -r '.github_relatedness.cli_unrelated_score.value' <<< "$_result")"
_nonsense_score="$(jq -r '.content_quality.nonsense_score.value' <<< "$_result")"
_logical_continuity_score="$(jq -r '.content_quality.logical_continuity_score.value' <<< "$_result")"
_effort_score="$(jq -r '.content_quality.effort_score.value' <<< "$_result")"
_title_association_score="$(jq -r '.content_quality.title_logical_association_with_body_score.value' <<< "$_result")"

# Check if each score is higher than 7 (test fails if > 7)
MAX_SCORE=7
_template_match_fail=$([[ ${_template_match_score:-0} -gt $MAX_SCORE ]] && echo "FAIL" || echo "PASS")
_user_edits_fail=$([[ ${_user_edits_score:-0} -gt $MAX_SCORE ]] && echo "FAIL" || echo "PASS")
_github_unrelated_fail=$([[ ${_github_unrelated_score:-0} -gt $MAX_SCORE ]] && echo "FAIL" || echo "PASS")
_cli_unrelated_fail=$([[ ${_cli_unrelated_score:-0} -gt $MAX_SCORE ]] && echo "FAIL" || echo "PASS")
_nonsense_fail=$([[ ${_nonsense_score:-0} -gt $MAX_SCORE ]] && echo "FAIL" || echo "PASS")
_logical_continuity_fail=$([[ ${_logical_continuity_score:-0} -gt $MAX_SCORE ]] && echo "FAIL" || echo "PASS")
_effort_fail=$([[ ${_effort_score:-0} -gt $MAX_SCORE ]] && echo "FAIL" || echo "PASS")
_title_association_fail=$([[ ${_title_association_score:-0} -gt $MAX_SCORE ]] && echo "FAIL" || echo "PASS")

# Determine test suite results (FAIL if more than one individual test in the suite fails)
_template_similarity_result="PASS"
_template_fail_count=0
[[ "$_template_match_fail" == "FAIL" ]] && ((_template_fail_count++))
[[ "$_user_edits_fail" == "FAIL" ]] && ((_template_fail_count++))
if [[ $_template_fail_count -gt 1 ]]; then
    _template_similarity_result="FAIL"
fi

_github_relatedness_result="PASS"
_github_fail_count=0
[[ "$_github_unrelated_fail" == "FAIL" ]] && ((_github_fail_count++))
[[ "$_cli_unrelated_fail" == "FAIL" ]] && ((_github_fail_count++))
if [[ $_github_fail_count -gt 1 ]]; then
    _github_relatedness_result="FAIL"
fi

_content_quality_result="PASS"
_content_fail_count=0
[[ "$_nonsense_fail" == "FAIL" ]] && ((_content_fail_count++))
[[ "$_logical_continuity_fail" == "FAIL" ]] && ((_content_fail_count++))
[[ "$_effort_fail" == "FAIL" ]] && ((_content_fail_count++))
[[ "$_title_association_fail" == "FAIL" ]] && ((_content_fail_count++))
if [[ $_content_fail_count -gt 1 ]]; then
    _content_quality_result="FAIL"
fi

# Determine overall result (FAIL if any test suite fails)
_overall_result="PASS"
if [[ "$_template_similarity_result" == "FAIL" || "$_github_relatedness_result" == "FAIL" || "$_content_quality_result" == "FAIL" ]]; then
    _overall_result="FAIL"
fi

echo "=== SPAM DETECTION RESULTS ==="
echo
echo "Template Similarity: $_template_similarity_result"
echo "  Template Match Score: $_template_match_score ($_template_match_fail)"
echo "  User Edits Score: $_user_edits_score ($_user_edits_fail)"
echo
echo "GitHub Relatedness: $_github_relatedness_result"
echo "  GitHub Unrelated Score: $_github_unrelated_score ($_github_unrelated_fail)"
echo "  CLI Unrelated Score: $_cli_unrelated_score ($_cli_unrelated_fail)"
echo
echo "Content Quality: $_content_quality_result"
echo "  Nonsense Score: $_nonsense_score ($_nonsense_fail)"
echo "  Logical Continuity Score: $_logical_continuity_score ($_logical_continuity_fail)"
echo "  Effort Score: $_effort_score ($_effort_fail)"
echo "  Title Association Score: $_title_association_score ($_title_association_fail)"
echo
echo "Overall Result: $_overall_result"
# echo
# echo "Raw JSON Response:"
# echo "$_resp"