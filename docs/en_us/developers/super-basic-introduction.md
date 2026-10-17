# Ultra Basic Getting Started Guide

> **Who is this document for?**
>
> You opened [Quick Start](./getting-started.md), saw `git clone`, `pnpm install`, `Pipeline`, `PR`... and are completely lost, not knowing where to start.
>
> This document is for you — it doesn't teach you to write code; it teaches you "how to understand what others are talking about."
>
> If you already know how to use Git, the terminal, and VS Code, this is too basic for you. Go directly to [README.md](./README.md) → [getting-started.md](./getting-started.md).

---

## Chapter Zero · Figure Out Which Kind of Beginner You Are

| Your situation | Jump to where |
| -------------------------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| I just want to **use** MaaEnd for automation, not write code | → [Official Website Download](https://maaend.com/), you don't need developer documentation |
| I want to help write Pipeline (JSON configuration, no coding needed) | → Read this whole guide → [getting-started.md](./getting-started.md) |
| I want to write Go Service / modify underlying logic | → Read this whole guide → Learn Go basics → [getting-started.md](./getting-started.md) |

**The vast majority of contributors only take the Pipeline path. No programming skills or Go code writing required.**

---

## Chapter One · What Do All These Buzzwords Mean?

Before starting, let's explain common terms in the simplest language. Precision isn't needed, just enough to get the job done.

| Term | Plain English Explanation |
| ------------------------- | ------------------------------------------------------------------------------------------------------ |
| **Git** | A "save system" for code. Each save can have a note, and you can go back to old versions anytime |
| **GitHub** | A website that "puts Git saves online," where everyone can collaborate |
| **Terminal/Command Line** | That black box. Use typing instead of a mouse to operate the computer |
| **VS Code** | An enhanced notepad specifically for writing code and configuration files |
| **JSON** | A form-filling format. `{}` is a table, `[]` is a list |
| **Pipeline** | An assembly line. In order: recognize screen → act → recognize screen → act... like following a recipe |
| **Fork** | Copy someone else's repository to your own name |
| **Clone** | Download code from the internet to your computer |
| **Branch** | A branch. Open your own line, don't mess with others' |
| **Commit** | Save. Take a snapshot of current changes, write a line of notes |
| **Push** | Upload local saves to GitHub |
| **PR** | Pull Request. Send your changes to the project maintainers for review |
| **Template Matching** | Find a small image in a larger one. E.g., "find this button on the screen" |

---

## Chapter Two · What You Need to Install on Your Computer

### 2.1 Git

- Download: [git-scm.com](https://git-scm.com/downloads)
- Click "Next" all the way, no need to change any settings
- After installation, if "Git Bash Here" appears when right-clicking on the desktop, it's working

**Learning Git? Recommended resources, in order:**

1. [Learn Git Branching](https://learngitbranching.js.org/) — Highly recommended! Interactive challenges, learn while playing, also recommended in project documentation
2. [Pro Git](https://git-scm.com/book/en/v2) — The official book, very detailed; read this if you'd rather not play through the challenges

### 2.2 Terminal

- **Windows 11**: Right-click in a folder → "Open in Terminal"
- **Windows 10**: After installing Git, right-click → "Git Bash Here"
- **macOS**: `Command + Space` → Type `Terminal` → Enter

You only need to know two commands:

```bash
cd folder_name    # Enter a folder
ls             # See what's in the current folder
```

You can copy and paste the rest straight from the docs.

### 2.3 VS Code

- Download: [code.visualstudio.com](https://code.visualstudio.com/)
- During installation, it's recommended to check "Add to PATH" and "Add Code to right-click menu"
- VS Code itself just needs to be installed; plugins can be installed after cloning and opening the project folder — because `@recommended` workspace recommendations require a project to be open.
- Most important plugin: **Maa Pipeline Support** — for screenshots, selecting recognition areas (you can also search and install this in the Extensions marketplace first)

### 2.4 Node.js + pnpm

- [Node.js Official Website](https://nodejs.org/) Download the **LTS version** (22.x or higher), click next all the way
- After installation, open the terminal and type:

```bash
corepack enable pnpm
```

- Verify: `pnpm --version` shows a version number (requires 10+) and you're good
- pnpm is Node's package manager (it installs dependencies). Install it now and leave it; it is only used later, when you run `pnpm install` after cloning the project

### 2.5 uv + Python

- Install uv by following the [official installation guide](https://docs.astral.sh/uv/getting-started/installation/)
- uv prepares Python 3.10+ and script dependencies from the project's `pyproject.toml`; you do not need to create a virtual environment or run `pip install` manually

### 2.6 Go (Required)

The project's underlying logic depends on Go for compilation and execution, so **it must be installed**. Good news: you don't need to learn Go syntax or write Go code; just install it and leave it. Go to [go.dev](https://go.dev/) to download and install (1.25.6+). The part of the project written in Go is under `agent/go-service/`; you don't need to touch it for now.

> Everything installed above (Node / uv / Go / pnpm) **just needs to be installed for now** — Chapter Three only needs Git and VS Code; the rest won't be used until you actually run the project.

### 🎯 Checkpoint

> - [ ] Git is installed
> - [ ] Can open the terminal (can `cd` and `ls`)
> - [ ] VS Code is installed (recommended plugins come after cloning)
> - [ ] `node --version` has output
> - [ ] `pnpm --version` has output
> - [ ] `uv --version` has output
> - [ ] `go version` has output

---

## Chapter Three · Minimum GitHub Survival Guide

> Goal: fork + clone — copy the project to your own name, then download it locally. Creating branches, committing, pushing, and opening PRs are all part of submitting a PR, so they are left for Chapter Six, when you do a real task.
>
> Below are two paths, just choose one and stick with it:
>
> - **Route A: GitHub Desktop** — Graphical interface, all mouse clicks, suitable for pure beginners who don't want to touch the command line.
> - **Route B: Git Command Line** — Type git commands in the terminal, learn it and you can use it in any project.
>
> VS Code's built-in Git interface can also complete most operations, somewhere in between, but we won't expand on it here. Additionally, GitHub has a `gh` command-line tool (GitHub CLI) that can simplify operations like Fork / PR, feel free to explore it yourself.

---

### Fork — Copy the Repository to Your Own Name

This step is done on the webpage, and it is the same for both routes:

1. Open the [MaaEnd Repository](https://github.com/MaaEnd/MaaEnd), make sure you're logged in
2. Click the **Fork** button in the upper right corner
3. Don't change anything, just click **Create fork**
4. Wait a few seconds, the page will jump to `https://github.com/your-username/MaaEnd` — this is your own copy

> From now on, you make all your changes on this copy; when they are ready, you ask for them to be merged back into the original repository through a PR. This copy is what you clone below.

---

### Route A: GitHub Desktop (Graphical Interface)

First, go to [GitHub Desktop Official Website](https://desktop.github.com/) to download and install, then open it and log in with your GitHub account.

#### A.1 Clone — Download Locally

1. Open GitHub Desktop, menu bar **File → Clone a repository**
2. Select the **GitHub.com** tab, find the `your-username/MaaEnd` repository, click it
3. Choose a local storage path, click **Clone**
4. Wait for the download to complete, the repository is now on your computer

---

### Route B: Git Command Line

You've already installed Git. Open the terminal (right-click in a folder → "Git Bash Here" or "Open in Terminal") and follow along.

#### B.1 Clone — Download Locally

```bash
git clone --recursive https://github.com/your-username/MaaEnd.git
```

Replace `your-username` with your own GitHub username. Wait for it to finish, and a `MaaEnd` folder will appear in the current directory.

If you've already cloned but didn't use `--recursive`, you can run this command in the repository directory:

```bash
git submodule update --init --recursive
```

> **What are submodules?** MaaEnd references external resources (like model files) that are stored in other Git repositories. `--recursive` means "also download the referenced external repositories." Without it, some files will be missing, and subsequent operations won't work.

---

After cloning, both routes are the same: open the project folder in VS Code now, search `@recommended` in the Extensions view, and install the recommended plugins mentioned in [2.3](#23-vs-code).

### 🎯 Checkpoint

> - [ ] Forked MaaEnd on GitHub (copied it to your own name)
> - [ ] Cloned the copy locally
> - [ ] Opened the project in VS Code and installed the recommended plugins

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
            "template": "OutpostTrading/button.png",
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

> To read like plain language, the names above are simplified for illustration. Real Pipeline fields have fixed English names (`recognition` / `action` / `next`).

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
- [MDN JSON Tutorial](https://developer.mozilla.org/en-US/docs/Learn/JavaScript/Objects/JSON) — More systematic

### 🎯 Checkpoint

> - [ ] Can instantly distinguish `{}` from `[]`
> - [ ] Know names must use double quotes
> - [ ] Know you can't have a trailing comma
> - [ ] Wrote some JSON in VS Code and confirmed no red errors

---

## Chapter Five · How Pipelines Run

> This chapter will help you understand Pipelines written by others. The next chapter (Chapter Six) walks you through your first PR; after that, go to `getting-started.md`.

### 5.1 Core Idea: Look First, Then Act

Each Pipeline node does three things:

```text
┌──────────────────────────────────┐
│  Recognize (Look at the screen)  │  "Is what I'm looking for on the screen?"
├──────────────────────────────────┤
│  Act (Do something)              │  "Yes? Then click/swipe/press it!"
├──────────────────────────────────┤
│  Next Step (Jump)                │  "Then go to which node to continue?"
└──────────────────────────────────┘
```

> [!WARNING]
>
> ## **Iron Rule: Always recognize before acting.**
>
> ## You cannot assume "I clicked the button, the next screen will definitely appear" — you must confirm it visually every time

### 5.2 Breaking Down a Real Node

```json
{
    "EssenceFilterSwitchTab": {
        "desc": "Switch to the Weapon Essence tab",

        "recognition": {
            "type": "TemplateMatch",
            "param": {
                "template": "EssenceFilter/EssenceTabNotChoose.png",
                "roi": [
                    350,
                    0,
                    500,
                    80
                ],
                "threshold": 0.9
            }
        },

        "action": {
            "type": "Click"
        },

        "pre_delay": 0,
        "post_delay": 0,
        "post_wait_freezes": 400,

        "next": ["EssenceFilterCheckInInventory"]
    }
}
```

Translated line by line into plain English:

| Field | Plain English |
| ------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| `"desc"` | Comment for humans, ignored by the machine |
| `"recognition"` → `"type": "TemplateMatch"` | Recognition method: template matching (find a small image on the screen) |
| `"template"` | Where the image to find is stored |
| `"roi"` | Only search within this box — `[top-left x, top-left y, width, height]`, screen top-left corner is the origin |
| `"threshold": 0.9` | 90% similarity counts as a match |
| `"green_mask": true` (optional) | Green mask: If true, parts of the image you don't want to match are painted green RGB: (0, 255, 0), green areas are skipped during matching. Not used in this example; only needed when extra patterns in the template interfere |
| `"action"` → `"type": "Click"` | If recognized, click; defaults to clicking the recognized position |
| `"pre_delay": 0` | How many milliseconds to wait after recognition, before executing the action. This node's screen is usually stable, so set to 0 |
| `"post_delay": 0` | How many milliseconds to wait after executing the action, before recognizing next. Here `post_wait_freezes` is used instead |
| `"post_wait_freezes": 400` | After the action, wait until the recognized area has shown no significant change for 400 ms in a row, then continue. More reliable than a fixed `post_delay` |
| `"next": ["EssenceFilterCheckInInventory"]` | After completion, try the nodes in next one by one, and stop at the first one that matches |

> Use delay fields only when necessary: `pre_delay` and `post_delay` are fixed waits that do not look at the screen; to wait for the screen to stabilize, use `post_wait_freezes`. Most nodes can be set to 0. Clicking the node above triggers a tab-switch animation, so `post_wait_freezes: 400` is used to wait until the screen is fully stable.
>
> This only breaks down the most commonly used fields; there are many more available in practice — for the complete list of fields, see the [MaaFramework Pipeline Protocol](https://maafw.com/docs/3.1-PipelineProtocol/) (official documentation).

### 5.3 Next Step Jump Logic

```json
"next": ["EssenceFilterSwitchTab", "EssenceFilterInitNormalize", "[JumpBack]SceneEnterMenuValuables"]
```

The Pipeline will **try in order** — it tries the first one, and only tries the second if the first doesn't match. The list above is the `next` of `EssenceFilterMain`, the node before the one in 5.2: in the Valuables Vault with the Weapon Essence tab not yet selected, `EssenceFilterSwitchTab` matches and clicks it; with the tab already selected, `EssenceFilterInitNormalize` matches; only when neither matches (for example, you are not in the Valuables Vault) does the fallback `[JumpBack]SceneEnterMenuValuables` run: it enters the Valuables Vault, then comes back and tries the list in order again. So **put the most likely state first** and the fallback jump last. The more candidates, the better, so you can hit one in a single "screenshot → recognize → action" cycle.

### 5.4 Common Recognition Methods Quick Reference

| Method | Keyword | When to use |
| ------------------ | ---------------- | ----------------------------------------------------------------- |
| Template Matching | `TemplateMatch` | Find fixed icons, buttons — given an image, find it on the screen |
| OCR | `OCR` | Read text on the screen — e.g., confirm which screen you're on |
| Color Matching | `ColorMatch` | Detect the color of a specific point |
| All conditions met | `And` + `all_of` | Multiple conditions all met for a match |
| Any condition met | `Or` + `any_of` | One condition met is enough |

### 5.5 How to Capture a Recognition Template Yourself

> Prerequisite: start the game or emulator first and connect to it from the plugin's control panel, so the plugin can capture the real game screen.

1. Screenshots are based on 1280×720 as a baseline/recommended, but no need to manually switch resolution (framework will auto-scale)
2. In VS Code, `Ctrl+Shift+P` → `Maa: open crop tool` (requires the Maa Pipeline Support plugin)
3. Select the area you want to recognize on the screenshot
4. If needed, use a green mask to remove areas that interfere with recognition — paint parts you don't want to match green RGB: (0, 255, 0); green areas are skipped during matching. With the plugin installed, you can paint green directly on the screenshot; don't do it by hand in Photoshop
5. Save the processed screenshot as a PNG and put it in the `assets/resource/image/your-task-name/` folder (create it if it doesn't exist)

### 5.6 Where to Look Up Detailed Syntax

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

> Task: Add a button recognition to the project — one Pipeline node + one template image. No coding needed, anyone can do it.
>
> In Chapter Three you already forked and cloned your own copy (`your-username/MaaEnd`). This chapter goes through the whole contribution flow on that copy: create a branch → edit files → commit → push → open a PR. Opening the PR is done on the webpage; for creating the branch, committing and pushing, both a GitHub Desktop method and a terminal method are given (committing and pushing can also be done in VS Code), so pick whichever suits you.

### Step 0: Tell Git Who You Are (One-Time Setup)

A commit records "who made this save." **If you don't set this up, Git makes do with your computer's name, or refuses to commit and shows an error.** You only need to do this once; after that, it applies to every repository on this computer.

Open the terminal and type the two commands below (replace the text in quotes with your own name and email):

```bash
git config --global user.name "Your Name"
git config --global user.email "your-email@example.com"
```

> [!IMPORTANT]
> **Use the email address that is linked to your GitHub account** — GitHub uses the email to attribute commits to accounts: if it matches, the commit shows your avatar and counts toward your contribution stats; if it doesn't, the commit is attached to an "ownerless" account with a blank avatar, and the contribution is not credited to you.

> [!NOTE]
> **Don't want to expose your real email?** In GitHub → Settings → Emails, check "Keep my email addresses private" to get a noreply address like `12345678+your-username@users.noreply.github.com`. Set `user.email` to it: your commits are still linked to your account, but others can't see your real email.

Check that it is set correctly:

```bash
git config --global user.name
git config --global user.email
```

If they print the name and email you just entered, the setup worked.

> [!TIP]
> **Already committed before noticing it wasn't set, or set the wrong name?** Run the two commands above again with the correct values, then run `git commit --amend --reset-author` to update the author information of the last commit as well. This only changes the most recent commit; if that commit has already been pushed, you then need `git push --force-with-lease` to push it.

> [!NOTE]
> **I only commit with GitHub Desktop / VS Code. Do I still need to set this?**
> GitHub Desktop automatically uses your GitHub account information when committing, so no setup is needed. VS Code uses the git configuration above, so it's safer to set it anyway.

---

### Step 1: Create a Branch

> Fork copies the entire repository, Branch creates a working branch within the repository. v2 is this project's main branch (PRs are opened against it too). Never modify things directly on the v2 branch — create a branch, if it gets messed up, delete it, and v2 remains clean and unaffected.

Choose whichever method is easiest for you:

| Method | Operation |
| --- | --- |
| GitHub Desktop | Click the branch selection box at the top → **New Branch** → use English for the branch name, format suggestion `feat/description`, e.g., `feat/add-sell-button` → click **Create Branch** |
| Terminal | In the repository directory, type `git checkout -b feat/add-sell-button` |

### Step 2: Make the Changes — Write a Node + Make a Template

This change is a pair of files that go together, both under `assets/resource/`:

1. **Template image**: Follow [5.5 How to Capture a Recognition Template Yourself](#55-how-to-capture-a-recognition-template-yourself) to take a screenshot and turn it into a recognition template, save it as a PNG, and put it in the `assets/resource/image/your-task-name/` folder (use an English name, e.g. `OutpostTrading/`; the image could be `SellButton.png`)
2. **JSON node**: Create `your-task-name.json` under `assets/resource/pipeline/` and write a Pipeline node whose `template` in `recognition` points to the image you just made. What a node looks like and what each field does was just covered in [5.2](#52-breaking-down-a-real-node); just follow it

### Step 3: Commit — Save

Choose whichever method is easiest for you:

| Method | Operation |
| --- | --- |
| GitHub Desktop | All changes are listed on the left → check the files → write the commit message in the **Summary** box in the lower left corner → click **Commit to your-branch-name** |
| Terminal | `git add .` then `git commit -m "feat(task-name): what you did"` |
| VS Code | `Ctrl + Shift + G` → Click `+` to stage → Write commit message → Click `✓` |

Write the message following "Commit Message Format" below, e.g. `feat(OutpostTrading): Add sell button recognition template`.

### Step 4: Push — Upload to GitHub

Choose whichever method is easiest for you:

| Method | Operation |
| --- | --- |
| GitHub Desktop | On the first push of a new branch, the button at the top reads **Publish branch**; click it. Later pushes use **Push origin** |
| Terminal | `git push -u origin feat/add-sell-button` |
| VS Code | On the first push of a new branch, click **Publish Branch** in the Source Control view; later pushes use "Sync Changes" |

**Why `-u`?** Your locally created branch doesn't exist on GitHub yet. `-u` (short for `--set-upstream`) does two things:

1. Creates a remote branch with the same name on GitHub and uploads the local code
2. Makes the local branch "remember" which remote branch it corresponds to — after this, you can just use `git push` without typing a long command

**What if you forgot to add `-u`?** You'll get an error when pushing:

```text
fatal: The current branch feat/xxx has no upstream branch.
```

Don't panic, just type what it suggests:

```bash
git push --set-upstream origin feat/add-sell-button
```

The effect is the same as `-u`. After that, you only need `git push`. The first push might be slow, after that it's fast.

### Step 5: Open PR (On the Webpage)

1. Open **your forked repository** (`github.com/your-username/MaaEnd`), at the top of the page there will be a yellow prompt bar → click "Compare & pull request"
2. Confirm the base branch is `v2` (the main branch of the original repository), the head branch is the one you just pushed
3. Write a clear title: `feat(task-name): Added recognition template for some button`
4. If not done, select "Create draft pull request"
5. Click "Create pull request"

### Commit Message Format

This project follows [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/). The format is `type(scope): description`, and the scope is optional. The available types are defined by [`.github/cliff.toml`](../../../.github/cliff.toml); for the full details, including which changelog section each type goes into, see [getting-started.md § 0. Commit Conventions](./getting-started.md#commit-conventions-conventional-commits). Below is a quick reference for the prefixes:

| Prefix | When to use |
| --- | --- |
| `feat:` | New features (Pipeline nodes, recognition templates, etc.) |
| `fix:` | Bug fixes |
| `refactor:` | Restructuring code without changing behavior |
| `perf:` | Improving existing behavior (speed, accuracy, fault tolerance, etc.) |
| `docs:` | Documentation changes only |
| `style:` | Formatting/whitespace adjustments (doesn't affect code meaning) |
| `test:` | Adding or changing tests |
| `ci:` | CI workflows and configuration |
| `chore:` | Build, auxiliary tools, and other miscellaneous; use `chore(deps):` for dependency updates |
| `ai:` | AI collaboration such as skills and `AGENTS.md` |
| `revert:` | Reverting a previous commit |

Examples: `feat(OutpostTrading): Add sell button recognition template`, `fix: Fix startup crash`.

### Then What

- The Maintainer will review, may leave comments with modification suggestions
- You make changes locally → commit → push, the PR updates automatically
- Once approved, it gets merged in 🎉

### Full Process Review

```text
Fork the repository (done in Chapter Three)
    ↓
Clone your own copy (done in Chapter Three)
    ↓
Tell Git who you are (Step 0, one-time setup)
    ↓
Create a branch (open your own line)
    ↓
Make the template image and put it in assets/resource/image/your-task-name/
    ↓
Write a JSON node that references it (assets/resource/pipeline/your-task-name.json)
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

> Prerequisites (done in Chapter Three): forked your own copy and cloned it locally
>
> - [ ] Created a branch
> - [ ] Wrote a JSON node + added a template image
> - [ ] Commit + Push successful
> - [ ] See your PR on GitHub
> - [ ] 🎉 Congratulations! Your first open-source contribution!

---

## What to Read Next

After completing this guide, proceed in order:

| Order | Document | What you'll learn |
| ----- | -------------------------------------------- | --------------------------------------------------------------------- |
| 1 | [getting-started.md](./getting-started.md) | Set up the environment, get it running, complete a full Pipeline task |
| 2 | [components-guide.md](./components-guide.md) | Project architecture, reusable nodes |
| 3 | [tools-and-debug.md](./tools-and-debug.md) | Debugging tools, Maa Pipeline Support usage |
| 4 | [coding-standards.md](./coding-standards.md) | Coding standards, must-read before committing |

> [!NOTE]
> **External Resources**
>
> The following links point to independent projects or third-party services outside of MaaEnd, for extended reference.

- [MaaFramework Official Website](https://maafw.com/) — MaaEnd's underlying framework
- [MaaFramework Pipeline Protocol](https://maafw.com/docs/3.1-PipelineProtocol/) — Detailed syntax for all nodes
- [DeepWiki — MaaEnd](https://deepwiki.com/MaaEnd/MaaEnd) — AI-driven third-party online documentation browser

## Need Help?

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
