# Contributing to MaaEnd

[中文](./CONTRIBUTING.md)

This document is for anyone considering opening an issue, starting a discussion, or submitting a pull request. For technical details on development, see the [developer documentation](./docs/en_us/developers/README.md).

MaaEnd itself is developed with plenty of AI assistance, and we welcome AI. But every issue and PR is read and reviewed by humans, and showing up with work you aren't sure about pushes the entire burden of validation onto the maintainers. The rules below are about people, not tools.

## You Must Understand Your Code

Using AI to write code is fine, but if you can't explain what your changes do and how they interact with the greater system without the aid of AI, do not submit them. Specifically:

- **Fully understand it**: be able to debug it on your own and explain any change to a reviewer without AI.
- **Own maintenance**: game updates often change the UI, and features have to be adjusted along with them. You can't maintain code you don't understand.
- **Respect maintainers' time**: before submitting, make sure the change is actually needed and fits the project architecture.

## Submitting a PR

- Search existing issues and PRs first to make sure nobody is already working on the same thing. For new features, open an issue or discuss in the community group first, and start only after the direction is confirmed.
- Read the [Coding Standards](./docs/en_us/developers/coding-standards.md).
- Keep it simple: a simple solution that does 90% of the job is often better than a complex one that does 100%.

PRs we love:

- Fixing or extending the docs, for example correcting outdated instructions or adding usage examples for a component
- Fixing bugs, for example fixing a node that fails to recognize on a specific screen
- Small enhancements that make existing tasks work in more scenarios, for example supporting more platforms or controllers
- Small features, for example adding an option to an existing task
- Using Pipeline for the flow, for example screen transitions and clicks; Custom only handles complex recognition and algorithms

PRs we don't like:

- Changes whose complexity outweighs their benefit, for example changing large amounts of existing code for a small feature, or giant refactorings
- Unverified Pipeline changes, for example not debugged node by node in [MaaDebugger](https://github.com/MaaXYZ/MaaDebugger) or [Maa Pipeline Support](https://github.com/neko-para/maa-support-extension), only blindly run in MXU
- Using Custom for the flow, for example writing lots of screen transitions and clicks in Go
- Changes the author doesn't understand, for example submitting AI-generated code you can't explain

## AI Usage Policy

- You may use AI to learn the codebase, write code from a design you own, handle mechanical tasks such as formatting, and draft documentation for modules you already understand.
- You may use AI to draft the PR description, but you must check it for technical accuracy and trim it down.
- AI-generated content in issues and discussions must be reviewed and trimmed by you before it is submitted.
- **Replies to review comments and maintainers' questions must be written by you.** Using AI to polish grammar is fine, but the substance must be your own.
- **We do not accept PRs or comments submitted automatically by AI without human review.**
