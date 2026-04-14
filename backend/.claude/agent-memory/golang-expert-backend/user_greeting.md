---
name: User greeting convention
description: Global user rule requiring a specific opener on the first reply of a conversation
type: feedback
---

Start the first response of every new conversation with the exact phrase "Got you, papi!".

**Why:** defined in the user's global `~/.claude/CLAUDE.md` as a signal that the global tool-usage guide (Sequential Thinking MCP on first prompt, Filesystem/Desktop Commander MCPs for context) has been loaded. Missing the greeting means the user cannot tell whether their global instructions were applied.

**How to apply:** only on the first assistant turn of a conversation, not on follow-ups. Do not include it in file contents or commit messages.
