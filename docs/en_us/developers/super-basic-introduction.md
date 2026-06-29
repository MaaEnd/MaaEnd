# Ultra Basic Getting Started Guide

> **Who is this document for?**
>
> You opened [Getting Started](./getting-started.md), saw `git clone`, `pnpm install`, `Pipeline`, `PR`... and are completely lost, not knowing where to start.
>
> This document is for you — it doesn't teach you to write code; it teaches you "how to understand what others are talking about."
>
> If you already know how to use Git, the terminal, and VS Code, this is too basic for you. Go directly to [README.md](./README.md) → [getting-started.md](./getting-started.md).

---

## Chapter Zero · Figure Out Which Kind of Beginner You Are

| Your situation                                                       | Jump to where                                                                              |
| -------------------------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| I just want to **use** MaaEnd for automation, not write code         | → [Official Website Download](https://maaend.com/), you don't need developer documentation |
| I want to help write Pipeline (JSON configuration, no coding needed) | → Read this whole guide → [getting-started.md](./getting-started.md)                       |
| I want to write Go Service / modify underlying logic                 | → Read this whole guide → Learn Go basics → [getting-started.md](./getting-started.md)     |

**The vast majority of contributors only take the Pipeline path. No programming skills or Go code writing required.**

---

## Chapter One · What Do All These Buzzwords Mean?

Before starting, let's explain common terms in the simplest language. Precision isn't needed, just enough to get the job done.

| Term                      | Plain English Explanation                                                                              |
| ------------------------- | ------------------------------------------------------------------------------------------------------ |
| **Git**                   | A "save system" for code. Each save can have a note, and you can go back to old versions anytime       |
| **GitHub**                | A website that "puts Git saves online," where everyone can collaborate                                 |
| **Terminal/Command Line** | That black box. Use typing instead of a mouse to operate the computer                                  |
| **VS Code**               | An enhanced notepad specifically for writing code and configuration files                              |
| **JSON**                  | A form-filling format. `{}` is a table, `[]` is a list                                                 |
| **Pipeline**              | An assembly line. In order: recognize screen → act → recognize screen → act... like following a recipe |
| **Fork**                  | Copy someone else's repository to your own name                                                        |
| **Clone**                 | Download code from the internet to your computer                                                       |
| **Branch**                | A branch. Open your own line, don't mess with others'                                                  |
| **Commit**                | Save. Take a snapshot of current changes, write a line of notes                                        |
| **Push**                  | Upload local saves to GitHub                                                                           |
| **PR**                    | Pull Request. Send your changes to the project manager for review                                      |
| **Template Matching**     | Find a small image in a larger one. E.g., "find this button on the screen"                             |

---

## Chapter Two · What You Need to Install on Your Computer

### 2.1 Git

- Download: [git-scm.com](https://git-scm.com/downloads)
- Click "Next" all the way, no need to change any settings
- After installation, if "Git Bash Here" appears when right-clicking on the desktop, it's working

**Learning Git? Recommended resources, in order:**

1. [Learn Git Branching](https://learngitbranching.js.org/) — Highly recommended! Interactive challenges, learn while playing, also recommended in project documentation
2. [Pro Git](https://git-scm.com/book/en/v2) — The official book, thorough and well-written

### 2.2 Terminal

- **Windows 11**: Right-click in a folder → "Open in Terminal"
- **Windows 10**: After installing Git, right-click → "Git Bash Here"
- **macOS**: `Command + Space` → Type `Terminal` → Enter
- **Linux**: `Ctrl + Alt + T`

You only need to know three commands:

```bash
cd folder_name    # Enter a folder
ls             # See what's in the current folder
# Copy and paste     # Paste commands from the tutorial and press Enter
```

That's enough.

### 2.3 VS Code

- Download: [code.visualstudio.com](https://code.visualstudio.com/)
- During installation, it's recommended to check "Add to PATH" and "Add Code to right-click menu"
- VS Code itself just needs to be installed; plugins can be installed after cloning and opening the project folder — because `@recommended` workspace recommendations require a project to be open. See [B.2 Clone — Download Locally](#b2-clone--download-locally)
- Most important plugin: **Maa Pipeline Support** — for screenshots, selecting recognition areas (you can also search and install this in the Extensions marketplace first)

### 2.4 Node.js + pnpm

- [Node.js Official Website](https://nodejs.org/) Download the **LTS version** (22.x or higher), click next all the way
- After installation, open the terminal and type:

```bash
corepack enable pnpm
```

- Verify: `pnpm --version` shows a version number (requires 10+) and you're good

### 2.5 Python

- [Python Official Website](https://www.python.org/) Download version 3.10 or higher
- **You must check "Add Python to PATH" during installation!** Otherwise, the terminal won't find python

### 2.6 Go (Required)

The project's underlying logic depends on Go for compilation and execution, so **it must be installed**. Good news: you don't need to learn Go syntax or write Go code; just install it and leave it. Go to [go.dev](https://go.dev/) to download and install (1.25.6+).

### 🎯 Checkpoint

> - [ ] Git is installed
> - [ ] Can open the terminal (can `cd` and `ls`)
> - [ ] VS Code is installed, recommended plugins installed
> - [ ] `node --version` has output
> - [ ] `pnpm --version` has output
> - [ ] `python --version` has output
> - [ ] `go version` has output

---

## Chapter Three · Minimum GitHub Survival Guide

> Goal: Be able to clone, create branches, commit, push, and open PRs.
>
> Below are two paths, just choose one and stick with it:
>
> - **Route A: GitHub Desktop** — Graphical interface, all mouse clicks, suitable for pure beginners who don't want to touch the command line.
> - **Route B: Git Command Line** — Type git commands in the terminal, learn it and you can use it in any project.
>
> VS Code's built-in Git interface can also complete most operations, somewhere in between, but we won't expand on it here. Additionally, GitHub has a `gh` command-line tool (GitHub CLI) that can simplify operations like Fork / PR, feel free to explore it yourself.

---

### Route A: GitHub Desktop (Graphical Interface)

First, go to [GitHub Desktop Official Website](https://desktop.github.com/) to download and install, then open it and log in with your GitHub account.

#### A.1 Fork — Copy the Repository to Your Own Name

This step is done on the webpage:

1. Open the [MaaEnd Repository](https://github.com/MaaEnd/MaaEnd), make sure you're logged in
2. Click the **Fork** button in the upper right corner
3. Don't change anything, just click **Create fork**
4. Wait a few seconds, the page will jump to `https://github.com/your-username/MaaEnd` — this is your own copy

#### A.2 Clone — Download Locally

1. Open GitHub Desktop, menu bar **File → Clone a repository**
2. Select the **GitHub.com** tab, find the `your-username/MaaEnd` repository, click it
3. Choose a local storage path, click **Clone**
4. Wait for the download to complete, the repository is now on your computer

#### A.3 Branch — Create a Working Branch

1. In GitHub Desktop, there's a branch selection box at the top, click it
2. Select **New Branch**
3. Use English for the branch name, format suggestion `feat/description`, e.g., `feat/add-sell-button`, click **Create Branch**

> Fork copies the entire repository, Branch creates a working branch within the repository. Never modify things directly on the v2 branch — create a branch, if it gets messed up, delete it, and v2 remains clean and unaffected.

#### A.4 Commit — Save

1. After modifying files, GitHub Desktop will list all changes on the left
2. Check the files you want to save
3. In the lower left corner **Summary** input box, write the commit message (format see "Commit Message Format" below)
4. Click **Commit to your-branch-name**

#### A.5 Push — Upload to GitHub

A **Push origin** button will appear at the top of GitHub Desktop, just click it. The first push might be slow, after that it's fast.

#### A.6 Open PR — Request Review

1. After pushing, a **Create Pull Request** button will appear at the top of GitHub Desktop; clicking it will jump to the browser
2. Or manually open `https://github.com/your-username/MaaEnd`; at the top of the page, there will be a yellow prompt bar "xxx had recent pushes", click **Compare & pull request**
3. Write a clear title about what you changed
4. If not done, check **Create draft pull request**
5. Click **Create pull request**

---

### Route B: Git Command Line

You've already installed Git in Chapter Two. Open the terminal (right-click in a folder → "Git Bash Here" or "Open in Terminal") and follow along.

#### B.1 Fork — Copy the Repository to Your Own Name

This step is also done on the webpage:

1. Open the [MaaEnd Repository](https://github.com/MaaEnd/MaaEnd), make sure you're logged in
2. Click the **Fork** button in the upper right corner, just click **Create fork**
3. Wait a few seconds, the page will jump to `https://github.com/your-username/MaaEnd`

#### B.2 Clone — Download Locally

```bash
git clone --recursive https://github.com/your-username/MaaEnd.git
```

Replace `your-username` with your own GitHub username. Wait for it to finish, and a `MaaEnd` folder will appear in the current directory.

If you've already cloned but didn't use `--recursive`, you can run this command in the repository directory:

```bash
git submodule update --init --recursive
```

> **What are submodules?** MaaEnd references external resources (like model files) that are stored in other Git repositories. `--recursive` means "also download the referenced external repositories." Without it, some files will be missing, and subsequent operations won't work.

#### B.3 Branch — Create a Working Branch

```bash
cd MaaEnd                                    # Enter the repository directory
git checkout -b feat/your-branch-name               # Create and switch to a new branch
```

Use English for the branch name, format suggestion `feat/description`, e.g., `feat/add-sell-button`.

> Fork copies the entire repository, Branch creates a working branch within the repository. Never modify things directly on the v2 branch — create a branch, if it gets messed up, delete it, and v2 remains clean and unaffected.

#### B.4 Commit — Save

```bash
git add .                                                # Stage all changes
git commit -m "feat(task-name): what you did"                     # Save + write notes
```

See below for commit message format. If you only want to save specific files, replace `git add .` with `git add file-path`.

#### B.5 Push — Upload to GitHub

```bash
git push -u origin feat/your-branch-name
```

**Why `-u`?** Your locally created branch doesn't exist on GitHub yet. `-u` (short for `--set-upstream`) does two things:

1. Creates a remote branch with the same name on GitHub and uploads the local code
2. Makes the local branch "remember" which remote branch it corresponds to — after this, you can just use `git push` without typing a long command

**What if you forgot to add `-u`?** You'll get an error when pushing:

```text
fatal: The current branch feat/xxx has no upstream branch.
```

Don't panic, just type what it suggests:

```bash
git push --set-upstream origin feat/your-branch-name
```

The effect is the same as `-u`. After that, you only need `git push`.

#### B.6 Open PR — Request Review

After pushing, open your browser and visit `https://github.com/your-username/MaaEnd`; at the top of the page, there will be a yellow prompt bar "xxx had recent pushes", click **Compare & pull request**. Write a clear title, if not done check **Create draft pull request**, click **Create pull request**.

---

### Commit Message Format (Common to Both Routes)

This project follows [Conventional Commits](https://www.conventionalcommits.org/zh-hans/v1.0.0/), see [getting-started.md § 0. Commit Guidelines](./getting-started.md) for details. Below is a quick reference for common prefixes:

| Prefix   | When to use                                                     |
| -------- | --------------------------------------------------------------- |
| `feat:`  | New features (Pipeline nodes, recognition templates, etc.)      |
| `fix:`   | Bug fixes                                                       |
| `docs:`  | Documentation changes only                                      |
| `style:` | Formatting/whitespace adjustments (doesn't affect code meaning) |
| `chore:` | Build, dependencies, and other miscellaneous                    |

Examples: `feat(SellProduct): Add sell button recognition template`, `fix: Fix startup crash`.

---

### 🎯 Checkpoint

> - [ ] Can clone a repository locally
> - [ ] Can create a branch
> - [ ] Can commit (write a message that follows the format)
> - [ ] Can push
> - [ ] Can open a Draft PR on the GitHub webpage

---

## Chapter Four · JSON Form-Filling Basics

> Pipelines are written in JSON. What is JSON? **A filled-out form.** It's not called a programming language; it's called a configuration format.

### 4.1 Curly Braces `{}` = A Form/Table

```json
{
    "name": "Zhang San",
    "age": 25,
    "can_code": false
}
```

- `{}` = This is a form (or an "object")
- `"name"` = The field name in the form, **must be enclosed in double quotes**
- `"Zhang San"` = Value, text uses double quotes, numbers don't, true/false uses `true` / `false`

### 4.2 Square Brackets `[]` = A List

```json
{
    "name": "Li Si",
    "skills": [
        "eating",
        "sleeping",
        "coding"
    ]
}
```

### 4.3 Nesting — Forms within Forms

```json
{
    "recognition": {
        "type": "template_matching",
        "param": {
            "template": "SellProduct/button.png",
            "threshold": 0.7
        }
    },
    "action": {
        "type": "click"
    },
    "next": [
        "sell_product",
        "exit"
    ]
}
```

This is the basic shape of a Pipeline node.

### 4.4 Three Most Common Beginner Mistakes

#### Mistake 1: Extra comma after the last element

```json
// ❌ Wrong
{
    "a": 1,
    "b": 2,
}

// ✅ Correct
{
    "a": 1,
    "b": 2
}
```

#### Mistake 2: Field name missing double quotes

```json
// ❌ Wrong
{
    name: "Zhang San"
}

// ✅ Correct
{
    "name": "Zhang San"
}
```

#### Mistake 3: Mismatched curly braces

```json
// ❌ Wrong — missing a }
{
    "a": {
        "b": 1
    }
```

> With VS Code and the recommended plugins installed, these issues will be automatically highlighted in red.

### Learning Resources

- [JSON Tutorial (Runoob)](https://www.runoob.com/json/json-tutorial.html) — In Chinese, short and concise
- [MDN JSON Tutorial](https://developer.mozilla.org/zh-CN/docs/Learn/JavaScript/Objects/JSON) — More systematic

### 🎯 Checkpoint

> - [ ] Can instantly distinguish `{}` from `[]`
> - [ ] Know names must use double quotes
> - [ ] Know you can't have a trailing comma
> - [ ] Wrote some JSON in VS Code and confirmed no red errors

---

## Chapter Five · How Pipelines Run

> This chapter will help you understand Pipelines written by others. After reading, go to `getting-started.md`.

### 5.1 Core Idea: Look First, Then Act

Each Pipeline node does three things:

```text
┌─────────────────┐
│  Recognize (Look at the screen)  │  "Is what I'm looking for on the screen?"
├─────────────────┤
│  Act (Do something)    │  "Yes? Then click/swipe/press it!"
├─────────────────┤
│  Next Step (Jump)  │  "Then go to which node to continue?"
└─────────────────┘
```

> [!WARNING]
>
> ## **Iron Rule: Always recognize before acting.**
>
> ## You cannot assume "I clicked the button, the next screen will definitely appear" — you must confirm it visually every time

### 5.2 Breaking Down a Real Node

```json
{
    "SellProductMain": {
        "desc": "When on the main screen, recognize the regional development entry and click to enter",

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

Translated line by line into plain English:

| Field                                       | Plain English                                                                                                                               |
| ------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| `"desc"`                                    | Comment for humans, ignored by the machine                                                                                                  |
| `"recognition"` → `"type": "TemplateMatch"` | Recognition method: template matching (find a small image on the screen)                                                                    |
| `"template"`                                | Where the image to find is stored                                                                                                           |
| `"roi"`                                     | Only search within this box — `[top-left x, top-left y, width, height]`, screen top-left corner is the origin                               |
| `"threshold": 0.7`                          | 70% similarity counts as a match                                                                                                            |
| `"green_mask": true`                        | Green mask: If true, parts of the image you don't want to match are painted green RGB: (0, 255, 0), green areas are skipped during matching |
| `"action"` → `"type": "Click"`              | If recognized, click; defaults to clicking the recognized position                                                                          |
| `"pre_delay": 0`                            | How many milliseconds to wait after recognition, before executing the action. Entry node screen is stable, set to 0                         |
| `"post_delay": 0`                           | How many milliseconds to wait after executing the action, before recognizing next. Here `post_wait_freezes` is used instead                 |
| `"post_wait_freezes": 100`                  | Wait for the screen to stop moving after executing the action, then wait an additional 100ms. More reliable than a fixed `post_delay`       |
| `"next": ["SellProductLoop"]`               | After completion, try each node in next in order, only execute the first one recognized                                                     |

> Use delay fields only when necessary: `pre_delay` waits for the screen to appear, `post_delay` waits for animations to finish, `post_wait_freezes` waits for the screen to stabilize. Most nodes can be set to 0. SellProductMain is the task entry, the screen itself is stable, so both pre/post_delay are 0.
>
> This only breaks down the most commonly used fields; there are many more available in practice — when you encounter unfamiliar ones, search online for **MaaFramework Pipeline Protocol**, the official documentation has a complete list (link in section 5.5).

### 5.3 Common Recognition Methods Quick Reference

| Method             | Keyword          | When to use                                                       |
| ------------------ | ---------------- | ----------------------------------------------------------------- |
| Template Matching  | `TemplateMatch`  | Find fixed icons, buttons — given an image, find it on the screen |
| OCR                | `OCR`            | Read text on the screen — e.g., confirm which screen you're on    |
| Color Matching     | `ColorMatch`     | Detect the color of a specific point                              |
| All conditions met | `And` + `all_of` | Multiple conditions all met for a match                           |
| Any condition met  | `Or` + `any_of`  | One condition met is enough                                       |

### 5.4 Next Step Jump Logic

```json
"next": ["SellProductStartSelling", "SellProductTaskEnd"]
```

The Pipeline will **try in order** — it tries the first one, only tries the second if the first doesn't match. So put the most likely state first. The more candidates, the better, so you can hit one in a single "screenshot → recognize → action" cycle.

### 5.5 Where to Look Up Detailed Syntax

- [MaaFramework Pipeline Protocol](https://maafw.com/docs/3.1-PipelineProtocol/) — Official complete documentation
- Fastest learning method: Open the JSON files others have written under `assets/resource/pipeline/`, learn line by line

### 🎯 Checkpoint

> - [ ] Know each node = Recognize → Act → Next Step
> - [ ] Know what TemplateMatch and OCR do respectively
> - [ ] Know the try order of the `next` list
> - [ ] Open a Pipeline JSON in the project, can roughly understand what it's doing
> - [ ] Go read `getting-started.md`, no longer feel like it's a foreign language

---

## Chapter Six · Your First PR (Step-by-Step)

> Task: Contribute a screenshot template to the project — zero programming barrier, anyone can do it.

### Step 1: Fork

1. Open the [MaaEnd Repository](https://github.com/MaaEnd/MaaEnd)
2. Click the **Fork** button in the upper right corner
3. Don't change anything, just click **Create fork**

Wait a few seconds, it will jump to `https://github.com/your-username/MaaEnd` — this is your own copy.

### Step 2: Clone Your Own Repository

> Forgot what clone means? → [Go back to Chapter 3 to review](#chapter-three--minimum-github-survival-guide)

VS Code → `F1` → `Git: Clone` → Enter **the address you forked**, not the original.

### Step 3: Create a Branch

Click the branch name in the lower left corner → "Create new branch" → `feat/add-template-xxx`

### Step 4: Screenshot + Place Template

1. Screenshots are based on 1280×720 as a baseline/recommended, but no need to manually switch resolution (framework will auto-scale)
2. In VS Code, `Ctrl+Shift+P` → `Maa: Screenshot` (requires Maa Pipeline Support installed)
3. Select the area you want to recognize on the screenshot
4. If needed, use a green mask to remove areas that interfere with recognition — paint parts you don't want to match green RGB: (0, 255, 0); green areas are skipped during matching. With VS Code's Maa Pipeline Support plugin, you can paint directly on the screenshot, no need for manual Photoshop
5. Place the image in `assets/resource/image/your-task-name/`

### Step 5: Commit

Choose whichever method is easiest for you:

| Method                       | Operation                                                                  |
| ---------------------------- | -------------------------------------------------------------------------- |
| VS Code Interface            | `Ctrl + Shift + G` → Click `+` to stage → Write commit message → Click `✓` |
| Terminal (Chapter 3 Route B) | `git add .` then `git commit -m "feat(task-name): what you did"`           |

### Step 6: Push

| Method                       | Operation                                                |
| ---------------------------- | -------------------------------------------------------- |
| VS Code Interface            | Click the "Sync Changes" button in the lower left corner |
| Terminal (Chapter 3 Route B) | `git push -u origin feat/add-template-xxx`               |

### Step 7: Open PR (On the Webpage)

1. Open [your forked repository](https://github.com/你的用户名/MaaEnd), at the top of the page there will be a yellow prompt bar → click "Compare & pull request"
2. Confirm the base branch is `v2` (the main branch of the original repository), the head branch is the one you just pushed
3. Write a clear title: `feat(task-name): Added recognition template for some button`
4. If not done, select "Create draft pull request"
5. Click "Create pull request"

### Then What

- The Maintainer will review, may leave comments with modification suggestions
- You make changes locally → commit → push, the PR updates automatically
- Once approved, it gets merged in 🎉

### Full Process Review

```text
Fork the repository
    ↓
Clone your own repository
    ↓
Create a branch (open your own line)
    ↓
Screenshot + select recognition area (Maa Pipeline Support plugin)
    ↓
Place the image in the corresponding folder under assets/resource/image/
    ↓
Commit (save)
    ↓
Push (upload)
    ↓
Open a PR for review
    ↓
Wait for review ✅
```

### 🎯 Checkpoint

> - [ ] Forked MaaEnd
> - [ ] Cloned it locally
> - [ ] Created a branch
> - [ ] Placed a screenshot template
> - [ ] Commit + Push successful
> - [ ] See your PR on GitHub
> - [ ] 🎉 Congratulations! Your first open-source contribution!

---

## What to Read Next

After completing this guide, proceed in order:

| Order | Document                                     | What you'll learn                                                     |
| ----- | -------------------------------------------- | --------------------------------------------------------------------- |
| 1     | [getting-started.md](./getting-started.md)   | Set up the environment, get it running, complete a full Pipeline task |
| 2     | [components-guide.md](./components-guide.md) | Project architecture, reusable nodes                                  |
| 3     | [tools-and-debug.md](./tools-and-debug.md)   | Debugging tools, Maa Pipeline Support usage                           |
| 4     | [coding-standards.md](./coding-standards.md) | Coding standards, must-read before committing                         |

> [!NOTE]
> **External Resources**

> The following links point to independent projects or third-party services outside of MaaEnd, for extended reference.

- [MaaFramework Official Website](https://maafw.com/) — MaaEnd's underlying framework
- [MaaFramework Pipeline Protocol](https://maafw.com/docs/3.1-PipelineProtocol/) — Detailed syntax for all nodes
- [DeepWiki — MaaEnd](https://deepwiki.com/MaaEnd/MaaEnd) — AI-driven third-party online documentation browser

Need help?

If you encounter something you don't understand, don't panic and don't rush to ask everywhere. Try this order:

1. **Search** — Throw the error message or keywords into a search engine, [DeepSeek](https://www.deepseek.com/), eight times out of ten you'll find the answer directly
2. **Look** — Open the JSON files others have written under `assets/resource/pipeline/`, learn line by line; among the thousands of nodes in MaaEnd, someone has likely already written what you need
3. **Break it down** — Break the problem into smaller parts. Don't ask "how to write this task," ask "how to recognize this button" or "how to click after recognition" — break it down to the smallest questions, each smaller question is easier to search for
4. **Try** — Change a number, delete a field, run it and see the effect. Pipelines won't break; if it crashes, just change it back
5. **Ask** — If you've tried all the above and are still stuck, then go ask in the group or in an Issue. When asking, include what you've tried, what errors you got, and screenshots; don't just throw out "it doesn't work"

> Standing still waiting for others to feed you answers, versus jumping in and figuring it out yourself — the gap is much larger than you think.

---

> [!NOTE]
> **Final Words**
>
> You don't need to learn everything from start to finish before getting hands-on. The best way to learn:
>
> 1. Open JSON files others have written and read them
> 2. Change a number and try it
> 3. Run it and see the effect
> 4. If it errors, then check the documentation
>
> You'll never learn to swim by just watching. Get in the water.
