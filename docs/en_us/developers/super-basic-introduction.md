# Super Basic Introduction Guide

> **Who is this document for?**
>
> You opened [Getting Started](./getting-started.md) and saw `git clone`, `pnpm install`, `Pipeline`, `PR`... and had no idea where to begin.
>
> This document is written for you — it doesn't teach you to code, it teaches you "how to understand what everyone is talking about."
>
> If you already know Git, the terminal, and VS Code, this is too basic for you. Go straight to [README.md](./README.md) → [getting-started.md](./getting-started.md).

---

## Chapter 0 · Figure Out What Kind of Beginner You Are

| Your situation                                                         | Where to go                                                                        |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| I just want to **use** MaaEnd for auto-farming, not here to write code | → [Official website](https://maaend.com/), you don't need developer docs           |
| I want to help write Pipeline (JSON config, no coding required)        | → Finish this guide → [getting-started.md](./getting-started.md)                   |
| I want to write Go Service / modify core logic                         | → Finish this guide → Learn Go basics → [getting-started.md](./getting-started.md) |

**The vast majority of contributors only take the Pipeline route. No programming needed, no Go code required.**

---

## Chapter 1 · What All This Jargon Actually Means

Before we start, let's explain common terms in plain language. No need for precision — just enough to get things done.

| Term                        | Plain English                                                                                          |
| --------------------------- | ------------------------------------------------------------------------------------------------------ |
| **Git**                     | A "save system" for code. Every save can have a note, and you can go back to any old version anytime   |
| **GitHub**                  | A website that "puts Git saves online" so everyone can collaborate                                     |
| **Terminal / Command Line** | That black box. You type instead of clicking to control the computer                                   |
| **VS Code**                 | A notepad on steroids, built for writing code and config files                                         |
| **JSON**                    | A form-filling format. `{}` is a record, `[]` is a list                                                |
| **Pipeline**                | An assembly line. In order: recognize screen → act → recognize screen → act... like following a recipe |
| **Fork**                    | Copy someone else's repository to your own account                                                     |
| **Clone**                   | Download code from the internet to your computer                                                       |
| **Branch**                  | A separate line of work — make your changes without messing up others'                                 |
| **Commit**                  | Save. Take a snapshot of your changes, write a note                                                    |
| **Push**                    | Upload your local saves to GitHub                                                                      |
| **PR**                      | Pull Request. Submit your changes to the project maintainers for review                                |
| **Template Matching**       | Finding a small image inside a bigger one. Like "find this button on the screen"                       |

---

## Chapter 2 · What You Need on Your Computer

### 2.1 Git

- Download: [git-scm.com](https://git-scm.com/downloads)
- Click "Next" all the way through — no need to change any settings
- After installation, right-click on your desktop and see "Git Bash Here" — that means it worked

**Want to learn Git? These two are recommended, in order:**

1. [Learn Git Branching](https://learngitbranching.js.org/) — Highly recommended! Interactive game-based learning. The project docs recommend this too
2. [Pro Git](https://git-scm.com/book/en/v2) — The official book, thorough and well-written

### 2.2 Terminal

- **Windows 11**: Right-click in a folder → "Open in Terminal"
- **Windows 10**: After installing Git, right-click → "Git Bash Here"
- **macOS**: `Command + Space` → type `Terminal` → Enter
- **Linux**: `Ctrl + Alt + T`

You only need three commands:

```bash
cd folder-name    # Go into a folder
ls                # See what's in the current folder
# Copy-paste      # Paste commands from tutorials and press Enter
```

That's enough.

### 2.3 VS Code

- Download: [code.visualstudio.com](https://code.visualstudio.com/)
- During installation, check "Add to PATH" and "Add Code to context menu"
- Install VS Code itself first, add extensions later after you've cloned and opened the project folder — `@recommended` workspace recommendations only work when a project is open. See [B.2 Clone — Download to Your Computer](#b2-clone--download-to-your-computer) below
- Most important extension: **Maa Pipeline Support** — screenshots and ROI selection all depend on it (you can install it from the extension marketplace first)

### 2.4 Node.js + pnpm

- [Node.js website](https://nodejs.org/) — download the **LTS version** (22.x or higher), click through
- After installation, open a terminal and enter:

```bash
corepack enable pnpm
```

- Verify: `pnpm --version` shows a version number (10+ required) and you're good

### 2.5 Python

- [Python website](https://www.python.org/) — download 3.10 or higher
- **Make sure to check "Add Python to PATH" during installation!** Otherwise the terminal won't find python

### 2.6 Go (Required)

The project's core depends on Go for compilation and runtime, so it **must be installed**. Good news: you don't need to learn Go syntax or write Go code — just install it. Go to [go.dev](https://go.dev/) to download and install (1.25.6+).

### 🎯 Checkpoint

> - [ ] Git is installed
> - [ ] You can open a terminal (know `cd` and `ls`)
> - [ ] VS Code is installed, recommended extensions are installed
> - [ ] `node --version` gives output
> - [ ] `pnpm --version` gives output
> - [ ] `python --version` gives output
> - [ ] `go version` gives output

---

## Chapter 3 · GitHub Minimum Survival Guide

> Goal: be able to clone, create branches, commit, push, and open a PR.
>
> Two paths below — pick one and stick with it:
>
> - **Route A: GitHub Desktop** — Graphical interface, everything with mouse clicks. For pure beginners who don't want to touch the command line.
> - **Route B: Git Command Line** — Type git commands in the terminal. Once you learn it, you can use it in any project.
>
> VS Code's built-in Git interface can also do most operations and sits somewhere in between — not covered here. GitHub also has an official `gh` CLI tool (GitHub CLI) that simplifies Fork/PR operations — feel free to explore that on your own.

---

### Route A: GitHub Desktop (GUI)

First go to the [GitHub Desktop website](https://desktop.github.com/) to download and install. Open it and sign in with your GitHub account.

#### A.1 Fork — Copy the Repository to Your Account

This step is done in the browser:

1. Open the [MaaEnd repository](https://github.com/MaaEnd/MaaEnd), make sure you're signed in
2. Click the **Fork** button in the top right
3. Don't change anything, just click **Create fork**
4. Wait a few seconds — the page jumps to `https://github.com/your-username/MaaEnd` — this is your own copy

#### A.2 Clone — Download to Your Computer

1. Open GitHub Desktop, menu bar **File → Clone a repository**
2. Select the **GitHub.com** tab, find `your-username/MaaEnd`, click it
3. Choose a local path, click **Clone**
4. Wait for it to finish — the repository is now on your computer

#### A.3 Branch — Create a Working Branch

1. There's a branch selector at the top of GitHub Desktop — click it
2. Select **New Branch**
3. Use an English branch name — format suggestion: `feat/description`, e.g. `feat/add-sell-button`, then click **Create Branch**

> Fork copies the entire repository. Branch creates a separate line of work inside a repository. Never make changes directly on the v2 branch — create a branch. If you mess it up, just delete it. v2 stays clean and unaffected.

#### A.4 Commit — Save

1. After editing files, GitHub Desktop lists all changes on the left
2. Check the files you want to save
3. Write a commit message in the **Summary** box at the bottom left (see "Commit Message Format" below)
4. Click **Commit to your-branch-name**

#### A.5 Push — Upload to GitHub

A **Push origin** button appears at the top of GitHub Desktop — just click it. The first push is a bit slow; after that it's fast.

#### A.6 Open a PR — Request Review

1. After pushing, a **Create Pull Request** button appears at the top of GitHub Desktop — clicking it opens your browser
2. Or manually open `https://github.com/your-username/MaaEnd` — there'll be a yellow banner at the top saying "xxx had recent pushes" — click **Compare & pull request**
3. Write a clear title describing what you changed
4. If not done yet, check **Create draft pull request**
5. Click **Create pull request**

---

### Route B: Git Command Line

You already installed Git in Chapter 2. Open a terminal (right-click in folder → "Git Bash Here" or "Open in Terminal") and follow along.

#### B.1 Fork — Copy the Repository to Your Account

Also done in the browser:

1. Open the [MaaEnd repository](https://github.com/MaaEnd/MaaEnd), make sure you're signed in
2. Click the **Fork** button in the top right, then directly click **Create fork**
3. Wait a few seconds — the page jumps to `https://github.com/your-username/MaaEnd`

#### B.2 Clone — Download to Your Computer

```bash
git clone --recursive https://github.com/your-username/MaaEnd.git
```

Replace `your-username` with your actual GitHub username. Once it finishes, you'll have a `MaaEnd` folder in your current directory.

If you already cloned without `--recursive`, run this inside the repository:

```bash
git submodule update --init --recursive
```

> **What are submodules?** MaaEnd references external resources (like model files) that live in other Git repositories. `--recursive` means "download the referenced external repositories too." Without it, some files will be missing and things won't run properly.

#### B.3 Branch — Create a Working Branch

```bash
cd MaaEnd                                    # Enter the repository directory
git checkout -b feat/your-branch-name         # Create and switch to a new branch
```

Use an English branch name — format suggestion: `feat/description`, e.g. `feat/add-sell-button`.

> Fork copies the entire repository. Branch creates a separate line of work inside a repository. Never make changes directly on the v2 branch — create a branch. If you mess it up, just delete it. v2 stays clean and unaffected.

#### B.4 Commit — Save

```bash
git add .                                                # Stage all changes
git commit -m "feat(task-name): what you did"            # Save + write a note
```

Commit message format is below. To only save specific files, replace `git add .` with `git add file-path`.

#### B.5 Push — Upload to GitHub

```bash
git push -u origin feat/your-branch-name
```

**Why `-u`?** The branch you created locally doesn't exist on GitHub yet. `-u` (short for `--set-upstream`) does two things:

1. Creates a remote branch with the same name on GitHub and uploads your local code
2. Makes your local branch "remember" which remote branch it maps to — after this, just `git push` is enough, no long commands

**What if you forget `-u`?** Push will fail with:

```text
fatal: The current branch feat/xxx has no upstream branch.
```

Don't panic — just type what it tells you:

```bash
git push --set-upstream origin feat/your-branch-name
```

Same effect as `-u`. After that, `git push` alone will work.

#### B.6 Open a PR — Request Review

After pushing, open your browser to `https://github.com/your-username/MaaEnd`. There'll be a yellow banner at the top saying "xxx had recent pushes" — click **Compare & pull request**. Write a clear title. If not done, check **Create draft pull request**, then click **Create pull request**.

---

### Commit Message Format (Both Routes)

This project follows [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/). See [getting-started.md § 0. Commit Conventions](./getting-started.md) for details. Here's a quick reference:

| Prefix   | When to Use                                               |
| -------- | --------------------------------------------------------- |
| `feat:`  | New feature (Pipeline nodes, recognition templates, etc.) |
| `fix:`   | Bug fix                                                   |
| `docs:`  | Documentation-only changes                                |
| `style:` | Formatting/whitespace (no code meaning changes)           |
| `chore:` | Build, dependencies, misc.                                |

Examples: `feat(SellProduct): add sell button recognition template`, `fix: resolve startup crash`.

---

### 🎯 Checkpoint

> - [ ] Can clone a repository to local
> - [ ] Can create a branch
> - [ ] Can commit (with properly formatted messages)
> - [ ] Can push
> - [ ] Can open a Draft PR on the GitHub website

---

## Chapter 4 · JSON Form-Filling 101

> Pipelines are written in JSON. What is JSON? **A filled-in form.** It's not a programming language — it's a configuration format.

### 4.1 Curly Braces `{}` = A Record

```json
{
    "name": "John Doe",
    "age": 25,
    "canCode": false
}
```

- `{}` = This is a record (or an "object")
- `"name"` = A field name in the record — **must be wrapped in double quotes**
- `"John Doe"` = The value. Text uses double quotes, numbers don't, true/false use `true`/`false`

### 4.2 Square Brackets `[]` = A List

```json
{
    "name": "Jane Smith",
    "skills": [
        "eating",
        "sleeping",
        "coding"
    ]
}
```

### 4.3 Nesting — Records Inside Records

```json
{
    "recognition": {
        "type": "TemplateMatch",
        "param": {
            "template": "SellProduct/button.png",
            "threshold": 0.7
        }
    },
    "action": {
        "type": "Click"
    },
    "next": [
        "SellItem",
        "Exit"
    ]
}
```

This is the basic shape of a Pipeline node.

### 4.4 Three Most Common Beginner Mistakes

#### Mistake 1: Trailing Comma After the Last Element

```json
// ❌ Wrong
{
    "a": 1,
    "b": 2,
}

// ✅ Right
{
    "a": 1,
    "b": 2
}
```

#### Mistake 2: Forgetting Double Quotes Around Field Names

```json
// ❌ Wrong
{
    name: "John"
}

// ✅ Right
{
    "name": "John"
}
```

#### Mistake 3: Mismatched Braces

```json
// ❌ Wrong — missing one }
{
    "a": {
        "b": 1
    }
```

> With recommended VS Code extensions installed, these problems will all be highlighted in red automatically.

### Learning Resources

- [JSON Tutorial (MDN)](https://developer.mozilla.org/en-US/docs/Learn/JavaScript/Objects/JSON) — Thorough and well-structured
- [W3Schools JSON Tutorial](https://www.w3schools.com/js/js_json_intro.asp) — Short and beginner-friendly

### 🎯 Checkpoint

> - [ ] Can tell `{}` and `[]` apart at a glance
> - [ ] Know that field names must use double quotes
> - [ ] Know that trailing commas are not allowed
> - [ ] Can write a piece of JSON in VS Code with no red squiggles

---

## Chapter 5 · How Pipeline Works

> This chapter lets you read and understand Pipelines written by others. After this, go read `getting-started.md`.

### 5.1 Core Idea: Look First, Then Act

Every Pipeline node does three things:

```text
┌──────────────────────┐
│  Recognition (look)   │  "Is what I'm looking for on screen?"
├──────────────────────┤
│  Action (act)         │  "Yes? Then click/swipe/press it!"
├──────────────────────┤
│  Next (jump)          │  "Where to go next?"
└──────────────────────┘
```

> [!WARNING]
>
> ## **Golden Rule: Always recognize before acting.**
>
> ## Never assume "I clicked the button, so the next screen must appear" — always verify with your own eyes

### 5.2 Breaking Down a Real Node

```json
{
    "SellProductMain": {
        "desc": "On the main screen, find the Regional Development entrance and click it",

        "recognition": {
            "type": "TemplateMatch",
            "param": {
                "template": "SellProduct/RegionalDevelopmentEntry.png",
                "roi": [
                    400,
                    200,
                    480,
                    320
                ],
                "threshold": 0.7,
                "green_mask": true
            }
        },

        "action": {
            "type": "Click"
        },

        "pre_delay": 0,
        "post_delay": 0,
        "post_wait_freezes": 100,

        "next": ["SellProductLoop"]
    }
}
```

Translating each field into plain English:

| Field                                       | Plain English                                                                                                    |
| ------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| `"desc"`                                    | A human-readable comment — the machine ignores it                                                                |
| `"recognition"` → `"type": "TemplateMatch"` | Recognition method: Template Matching (find a small image on screen)                                             |
| `"template"`                                | Where the template image is stored                                                                               |
| `"roi"`                                     | Only search within this box — `[top-left x, top-left y, width, height]`, origin is top-left of screen            |
| `"threshold": 0.7`                          | 70% similarity counts as a match                                                                                 |
| `"green_mask": true`                        | Green mask: if true, paint unwanted regions in the image green RGB: (0, 255, 0) — matching skips green areas     |
| `"action"` → `"type": "Click"`              | If recognized, click it — defaults to clicking the recognized position                                           |
| `"pre_delay": 0`                            | How many ms to wait after recognition before performing the action. Entry node, screen is stable, so 0           |
| `"post_delay": 0`                           | How many ms to wait after action before recognizing `next`. Using `post_wait_freezes` instead here               |
| `"post_wait_freezes": 100`                  | After action, wait until the screen stops changing, then wait 100 ms more. More reliable than fixed `post_delay` |
| `"next": ["SellProductLoop"]`               | After finishing, try each node in `next` in order — execute the first one that matches                           |

> Delay fields are only used when necessary: `pre_delay` waits for a screen to appear, `post_delay` waits for an animation to finish, `post_wait_freezes` waits for the screen to stabilize. Most nodes can stay at 0. SellProductMain is a task entry point where the screen is already stable, so both pre/post_delay are 0.
>
> Only the most common fields are covered here — there are many more available. If you encounter an unfamiliar one, search for **MaaFramework Pipeline Protocol** online — the official docs have a complete list (see section 5.5 for links).

### 5.3 Common Recognition Methods Quick Reference

| Method            | Keyword          | When to Use                                                       |
| ----------------- | ---------------- | ----------------------------------------------------------------- |
| Template Matching | `TemplateMatch`  | Finding fixed icons/buttons — provide an image, find it on screen |
| OCR (Text)        | `OCR`            | Reading text on screen — e.g. confirming which screen you're on   |
| Color Matching    | `ColorMatch`     | Detecting the color at a specific point                           |
| All Of (AND)      | `And` + `all_of` | Multiple conditions must all match                                |
| Any Of (OR)       | `Or` + `any_of`  | Matching any one condition is enough                              |

### 5.4 Next-Jump Logic

```json
"next": ["SellProductStartSelling", "SellProductTaskEnd"]
```

The Pipeline **attempts in order** — tries the first node, and only moves to the next if the first doesn't match. So put the most likely state first. More candidates means better chances of matching within one "screenshot → recognize → act" cycle.

### 5.5 Where to Look Up Detailed Syntax

- [MaaFramework Pipeline Protocol](https://maafw.com/docs/3.1-PipelineProtocol/) — Official complete documentation
- The fastest way to learn: open the JSON files under `assets/resource/pipeline/` that others have written. Read one line, learn one line.

### 🎯 Checkpoint

> - [ ] Know each node = Recognition → Action → Next
> - [ ] Know what TemplateMatch and OCR are for
> - [ ] Know the try-order of the `next` list
> - [ ] Can open a Pipeline JSON in the project and roughly understand what it does
> - [ ] Go read `getting-started.md` — it no longer looks like gibberish

---

## Chapter 6 · Your First PR (Step by Step)

> Task: Contribute a screenshot template to the project — zero coding threshold, anyone can do it.

### Step 1: Fork

1. Open the [MaaEnd repository](https://github.com/MaaEnd/MaaEnd)
2. Click the **Fork** button in the top right
3. Don't change anything, just click **Create fork**

A few seconds later you're on `https://github.com/your-username/MaaEnd` — your own copy.

### Step 2: Clone Your Repository

> Forgot what clone means? → [Go back to Chapter 3 for a refresher](#chapter-3--github-minimum-survival-guide)

VS Code → `F1` → `Git: Clone` → Enter **your fork's URL**, not the original one.

### Step 3: Create a Branch

Click the branch name in the bottom-left corner → "Create new branch" → `feat/add-template-xxx`

### Step 4: Screenshot + Place Template

1. 1280×720 is the baseline/recommended resolution, but you don't need to manually switch — the framework auto-scales
2. In VS Code, `Ctrl+Shift+P` → `Maa: Screenshot` (requires Maa Pipeline Support installed)
3. Select the area you want to recognize on the screenshot
4. If needed, use green mask to cover distracting regions — paint the unwanted parts green RGB: (0, 255, 0). Matching skips green areas. With Maa Pipeline Support installed, you can paint directly on screenshots — no need for manual Photoshop
5. Place the image in `assets/resource/image/your-task-name/`

### Step 5: Commit

Pick whichever method you prefer:

| Method                       | How                                                                        |
| ---------------------------- | -------------------------------------------------------------------------- |
| VS Code UI                   | `Ctrl + Shift + G` → Click `+` to stage → Write commit message → Click `✓` |
| Terminal (Chapter 3 Route B) | `git add .` then `git commit -m "feat(task-name): what you did"`           |

### Step 6: Push

| Method                       | How                                                |
| ---------------------------- | -------------------------------------------------- |
| VS Code UI                   | Click the "Sync Changes" button at the bottom left |
| Terminal (Chapter 3 Route B) | `git push -u origin feat/add-template-xxx`         |

### Step 7: Open a PR (In the Browser)

1. Open [your forked repository](https://github.com/your-username/MaaEnd) — there'll be a yellow banner at the top → click "Compare & pull request"
2. Confirm the base branch is `v2` (the original repo's main branch) and the head branch is the one you just pushed
3. Write a clear title: `feat(task-name): added recognition template for XYZ button`
4. If not done, select "Create draft pull request"
5. Click "Create pull request"

### What Then?

- Maintainers will review and may leave comments with change suggestions
- You make changes locally → commit → push — the PR updates automatically
- Once approved, it gets merged 🎉

### Full Process Recap

```text
Fork the repository
    ↓
Clone your own repository
    ↓
Create a branch (your own line of work)
    ↓
Screenshot + select recognition area (Maa Pipeline Support extension)
    ↓
Place the image in assets/resource/image/ corresponding folder
    ↓
Commit (save)
    ↓
Push (upload)
    ↓
Open a PR and request review
    ↓
Wait for approval ✅
```

### 🎯 Checkpoint

> - [ ] Forked MaaEnd
> - [ ] Cloned to local
> - [ ] Created a branch
> - [ ] Placed a screenshot template
> - [ ] Commit + Push successful
> - [ ] See your own PR on GitHub
> - [ ] 🎉 Congrats! Your first open source contribution!

---

## What to Read Next

After finishing this introduction, continue in order:

| Order | Document                                     | What You'll Learn                                                     |
| ----- | -------------------------------------------- | --------------------------------------------------------------------- |
| 1     | [getting-started.md](./getting-started.md)   | Set up the environment, get it running, complete a full Pipeline task |
| 2     | [components-guide.md](./components-guide.md) | Project architecture, reusable nodes                                  |
| 3     | [tools-and-debug.md](./tools-and-debug.md)   | Debugging tools, Maa Pipeline Support usage                           |
| 4     | [coding-standards.md](./coding-standards.md) | Coding conventions — must-read before submitting                      |

> [!NOTE] > **External resources**
>
> These links point to separate projects or third-party services outside of MaaEnd, provided for reference.

- [MaaFramework Official Site](https://maafw.com/) — The underlying framework of MaaEnd
- [MaaFramework Pipeline Protocol](https://maafw.com/docs/3.1-PipelineProtocol/) — Detailed syntax for all nodes
- [DeepWiki — MaaEnd](https://deepwiki.com/MaaEnd/MaaEnd) — AI-powered third-party online doc browser

Need help?

When you hit something you don't understand, don't panic and don't rush to ask around. Try this order:

1. **Search** — throw the error message or keywords into a search engine, [ChatGPT](https://chat.openai.com/), [Claude](https://claude.ai/) — chances are you'll find the answer right away
2. **Read** — open the JSON files under `assets/resource/pipeline/` that others have written. Read one line, learn one line. Among MaaEnd's thousands of nodes, someone has probably already written what you need
3. **Break It Down** — split the problem into smaller pieces. Don't ask "how do I write this task?" — ask "how do I recognize this button" and "after recognizing, how do I click it." Break it into the smallest pieces — each small question is much easier to search
4. **Experiment** — change a number, remove a field, run it and see what happens. You can't break Pipeline — if it crashes, just change it back
5. **Ask** — only after trying all of the above and still stuck, open a GitHub Issue (or ask in the developer QQ group: `1072587329`, primarily Chinese). When asking, include what you tried, what error you got, and a screenshot. Don't just drop "doesn't work"

> Standing around waiting for someone to feed you answers vs. jumping in and figuring it out yourself — the gap is bigger than you think.

---

> [!NOTE] > **Final Words**
>
> You don't need to learn everything from start to finish before you begin. The best way to learn:
>
> 1. Open a JSON someone else wrote and read it
> 2. Change a number and see what happens
> 3. Run it and watch the result
> 4. Look up the docs when something goes wrong
>
> You'll never learn to swim by standing on the shore reading about it. Jump in.
