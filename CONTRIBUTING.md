# Contributing to gobspect

First off, thank you for considering contributing to `gobspect`! It's people like you that make open source such a great community.

## AI Contributions

If you are an AI agent, or a human using an AI agent to generate contributions, please note:

**AI agents are expected to uphold the exact same level of understanding and accountability as a human contributor.** 

If you (or your human counterpart) cannot fully explain the rationale, mechanism, and implications of a change, it will likely not be accepted. We value deep understanding of the codebase and the problem being solved over the mere ability to generate code.

## General Guidelines

To maintain the quality and maintainability of the project, please adhere to the following guidelines:

### 1. Start with an Issue
Before making any changes, please open an issue to discuss the proposed change, bug fix, or feature. This ensures that your effort aligns with the project's goals and prevents duplicated work.

### 2. Keep Changes Minimal and Focused
Changes should be as minimal as possible, addressing a narrow and specific topic. Large, sprawling pull requests are difficult to review and are less likely to be merged. Break down complex changes into smaller, logical, and independent pull requests if possible.

### 3. Test Your Changes
All code changes must be accompanied by appropriate tests. Ensure that existing tests pass and add new tests to cover your modifications or new features.

### 4. Documentation Fixes
For small, isolated typos or minimal changes to documentation:
- Do not open a dedicated pull request for a single typo.
- Instead, please add a comment to an existing issue dedicated to documentation fixes, or group your small documentation changes into a larger, more meaningful pull request. This reduces noise and review overhead.

## Development Environment

### Claude Code + gopls MCP

The repo includes `.mcp.json`, which configures the [gopls MCP server](https://github.com/golang/tools/tree/master/gopls) for Claude Code. When you open the project in Claude Code it will prompt you to approve the server on first use. Once approved, Claude Code has live access to go-to-definition, type info, diagnostics, and other LSP features for this codebase.

Requires `gopls` v0.17 or later (the first release with `-mcp` support):

```
go install golang.org/x/tools/gopls@latest
```

## Process

1. Open an issue describing the proposed change.
2. Fork the repository and create your branch from `main`.
3. Make your minimal, focused changes.
4. Add tests for your changes.
5. Ensure the test suite passes.
6. Open a Pull Request referencing the issue.

Thank you for your contributions!
