#!/bin/bash

#_issue_url="https://github.com/cli/cli/issues/11236"  # a template copy
# _issue_url="https://github.com/cli/cli/issues/11223"  # a link
#_issue_url="https://github.com/cli/cli/issues/11242" # two words

_issue_url=https://github.com/cli/cli/issues/11272 # legit, short
#_issue_url=https://github.com/cli/cli/issues/11239 # legit, oss community

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
            "definition of 10": "The issue matches the provided project template exactly and has no user edits.",
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
            "description": "A number describing how sensible and legible the issue content is.",
            "definition of 0": "The issue is completely sensible and legible.",
            "definition of 5": "The issue has some nonsensical elements, but is otherwise grounded.",
            "definition of 10": "The issue is complete nonsense and illegible.",
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
            "definition of 10": "The title is completely unrelated to the body content.",
            "value": 0
        }
    }
}
```

# Definitions of SPAM

## Project context

Issues related to the GitHub CLI tool are less likely to be spam.

This is the GitHub CLI (gh) project - a command-line tool for GitHub. Legitimate issues should be related to:

- Bug reports about the CLI tool functionality
- Feature requests for new CLI commands or improvements
- Documentation issues
- Installation or usage problems
- Questions about CLI behavior
- Sometimes GitHub-related issues that are relevant to the CLI context

## Special considerations

- Very short descriptions aren''t automatically spam if they contain relevant keywords or references.
- Foreign language content should be evaluated based on relevance, not just that the language is not English.
- Consider the effort required to write the issue - more effort usually indicates legitimacy.
- Template similarities should be weighted heavily as they often indicate low-effort submissions.

## Examples of legitimate content

Issues that match legitimate content are NOT spam.

- Clear description of a bug with steps to reproduce.
- Feature requests with detailed explanations and use cases.
- Documentation improvements with specific suggestions.
- Questions about usage with context and examples.
- Reports that reference specific code, files, or functionality.

## Examples of spam content

Issues that match spam content are likely spam.

- A description that is a copy of (or a small variation of) the issue templates defined under the "Issue templates" section below.
- An empty issue description.
- A description that contains only a single word or a few words, such as "bug", "help", "issue", "problem".
- A meaningless description that does not provide any useful information about the issue.
- A description that is just one or more links without any context or explanation.
- Generic placeholder text like "Lorem ipsum" or "test test test".
- Repetitive content (same word/phrase repeated multiple times).
- Content that appears to be copied from other sources without relevance to the project.
- Promotional content, advertisements, or unrelated marketing material.
- Content in languages that seem inappropriate for the project context.
- Issues that don''t relate to the project''s purpose (e.g., personal messages, off-topic discussions).

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
        "model": "openai/o1"
    }
'

_request_body="$(jq --arg content "$_prompt" --arg system "$_system_prompt" '.messages[0].content = $system | .messages[1].content = $content' <<< "$_request_body_tmpl")"

_resp="$(curl --silent -L \
  -X POST \
  -H "Accept: application/vnd.github+json" \
  -H "Authorization: Bearer $(gh auth token)" \
  -H "X-GitHub-Api-Version: 2022-11-28" \
  -H "Content-Type: application/json" \
  https://models.github.ai/inference/chat/completions \
  -d "$_request_body"
  )"

_result="$(jq -r '.choices[0].message.content' <<< "$_resp")"
echo "$_result"