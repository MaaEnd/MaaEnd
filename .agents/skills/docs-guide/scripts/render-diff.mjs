/**
 * 比较 Markdown 文件在 git 版本与工作区之间的渲染结果，确认格式化或修改没有改变显示效果。
 * 使用 markdownlint 自带的 micromark（CommonMark + GFM 表格 / 自动链接 / 脚注）渲染，
 * 不支持 GitHub 独有语法：提示块用 gh api markdown 确认，标题锚点用 MD051 或推送后的页面确认（见 SKILL.md「验证」）。
 * 运行方式（在仓库根目录）：
 *   node .agents/skills/docs-guide/scripts/render-diff.mjs [--ref <git-ref>] <file.md>...
 * --ref 默认为 HEAD。结果：
 *   identical          渲染结果完全相同
 *   whitespace-only    只差标签间空白
 *   tightness-only     只差列表松紧（列表项是否直接包一层 <p>），条目间距会变
 *   DIFFERENT          渲染内容不同，输出第一处差异；存在此结果时退出码为 1
 *   skipped            文件在该 ref 中不存在
 * --ref 无效、不在 git 仓库里或 git 出错时，输出错误信息并以退出码 2 结束。
 */
import { execFileSync } from "node:child_process";
import { readFileSync, realpathSync } from "node:fs";
import { createRequire } from "node:module";
import { pathToFileURL } from "node:url";

const args = process.argv.slice(2);
let ref = "HEAD";
const files = [];
for (let i = 0; i < args.length; i++) {
    if (args[i] === "--ref") {
        ref = args[++i];
    } else {
        // git show 只认正斜杠，Windows 下传入的反斜杠路径要先转换
        files.push(args[i].replaceAll("\\", "/"));
    }
}
if (!ref || files.length === 0) {
    console.error("用法: node .agents/skills/docs-guide/scripts/render-diff.mjs [--ref <git-ref>] <file.md>...");
    process.exit(2);
}

const git = (gitArgs) =>
    execFileSync("git", ["--literal-pathspecs", ...gitArgs], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
const fail = (message, error) => {
    console.error(message);
    const stderr = String(error?.stderr ?? "").trim();
    if (stderr) {
        console.error(stderr);
    }
    process.exit(2);
};

// ref 写错或不在 git 仓库里时直接报错，不能把所有文件都当成「不存在」跳过
try {
    git(["rev-parse", "--verify", "--quiet", `${ref}^{commit}`]);
} catch (error) {
    fail(`无法解析 git 引用：${ref}`, error);
}

// micromark 不是项目的直接依赖，经由 markdownlint-cli → markdownlint 解析，避免写死 pnpm 目录里的版本号；
// pnpm 的 node_modules 是符号链接，要先取真实路径再解析
const requireFromCli = createRequire(realpathSync(`${process.cwd()}/node_modules/markdownlint-cli/package.json`));
const requireFromLint = createRequire(realpathSync(requireFromCli.resolve("markdownlint/helpers")));
const load = (name) => import(pathToFileURL(requireFromLint.resolve(name)).href);
const { micromark } = await load("micromark");
const { gfmTable, gfmTableHtml } = await load("micromark-extension-gfm-table");
const { gfmAutolinkLiteral, gfmAutolinkLiteralHtml } = await load("micromark-extension-gfm-autolink-literal");
const { gfmFootnote, gfmFootnoteHtml } = await load("micromark-extension-gfm-footnote");

const render = (markdown) =>
    micromark(markdown, {
        allowDangerousHtml: true,
        extensions: [gfmTable(), gfmAutolinkLiteral(), gfmFootnote()],
        htmlExtensions: [gfmTableHtml(), gfmAutolinkLiteralHtml(), gfmFootnoteHtml()],
    });

const CODE_BLOCK = /<pre><code[^>]*>[\s\S]*?<\/code><\/pre>/g;
const codeBlocks = (html) => html.match(CODE_BLOCK) ?? [];
// 块级标签两侧的空白不影响显示；行内标签（code、a、em 等）两侧的空格会显示出来，保留
const BLOCK_TAG = /\s*(<\/?(?:li|ul|ol|p|h[1-6]|table|thead|tbody|tr|th|td|blockquote|pre|hr|br|div|details|summary|section)\b[^>]*>)\s*/g;
// 代码块单独逐字比较，其余部分把连续空白视为一个空格
const collapse = (html) =>
    html
        .replace(CODE_BLOCK, "<pre/>")
        .replace(/\s+/g, " ")
        .replace(BLOCK_TAG, "$1")
        .trim();
const VOID_TAGS = new Set(["area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "source", "track", "wbr"]);
// 只去掉列表项直接包着的 <p>，也就是列表松紧的差别；其他位置多出或少了 <p>（例如 <div> 里多了一个段落）算显示变化
const stripListParagraphs = (html) => {
    const open = [];
    return html.replace(/<(\/?)([A-Za-z][\w-]*)\b[^>]*>/g, (tag, closing, rawName) => {
        const name = rawName.toLowerCase();
        if (VOID_TAGS.has(name) || tag.endsWith("/>")) {
            return tag;
        }
        if (!closing) {
            const listParagraph = name === "p" && open.at(-1) === "li";
            open.push(listParagraph ? "li>p" : name);
            return listParagraph ? "" : tag;
        }
        // 原始 HTML 里可能有不配对的标签：弹到对应的开标签为止
        const index = open.findLastIndex((entry) => entry === name || (name === "p" && entry === "li>p"));
        if (index === -1) {
            return tag;
        }
        const removed = open[index] === "li>p";
        open.length = index;
        return removed ? "" : tag;
    });
};

const firstDifference = (before, after) => {
    let i = 0;
    while (i < before.length && before[i] === after[i]) {
        i++;
    }
    const from = Math.max(0, i - 80);
    return `  ${ref}: …${before.slice(from, i + 160)}\n  工作区: …${after.slice(from, i + 160)}`;
};

let failed = false;
for (const file of files) {
    let original;
    try {
        // ls-tree 对不存在的路径输出为空且正常退出，借此把「文件不在 ref 里」和其他 git 错误分开
        if (!git(["ls-tree", "--name-only", ref, "--", file]).trim()) {
            console.log(`skipped            ${file}（${ref} 中不存在）`);
            continue;
        }
        original = git(["show", `${ref}:./${file}`]);
    } catch (error) {
        fail(`读取 ${ref}:${file} 失败`, error);
    }
    const before = render(original);
    const after = render(readFileSync(file, "utf8"));

    let status;
    let detail = "";
    if (before === after) {
        status = "identical";
    } else if (JSON.stringify(codeBlocks(before)) !== JSON.stringify(codeBlocks(after))) {
        status = "DIFFERENT";
        detail = "  代码块内容或数量不同";
    } else if (collapse(before) === collapse(after)) {
        status = "whitespace-only";
    } else if (collapse(stripListParagraphs(before)) === collapse(stripListParagraphs(after))) {
        status = "tightness-only";
    } else {
        status = "DIFFERENT";
        detail = firstDifference(collapse(before), collapse(after));
    }
    console.log(`${status.padEnd(18)} ${file}`);
    if (detail) {
        console.log(detail);
        failed = true;
    }
}
process.exitCode = failed ? 1 : 0;
