/**
 * 比较 Markdown 文件在 git 版本与工作区之间的渲染结果，确认格式化或修改没有改变显示效果。
 * 使用 markdownlint 自带的 micromark（CommonMark + GFM 表格 / 自动链接 / 脚注）渲染，
 * 不支持 GitHub 独有语法：提示块用 gh api markdown 确认，标题锚点用 MD051 或推送后的页面确认（见 SKILL.md「验证」）。
 * 运行方式（在仓库根目录）：
 *   node .agents/skills/docs-guide/scripts/render-diff.mjs [--ref <git-ref>] <file.md>...
 * --ref 默认为 HEAD。结果：
 *   identical          渲染结果完全相同
 *   whitespace-only    只差标签间空白
 *   tightness-only     只差列表松紧（<li> 内是否包 <p>），条目间距会变
 *   DIFFERENT          渲染内容不同，输出第一处差异；存在此结果时退出码为 1
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
const stripParagraphs = (html) => collapse(html.replace(/<\/?p>/g, ""));

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
        original = execFileSync("git", ["show", `${ref}:./${file}`], { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] });
    } catch {
        console.log(`skipped            ${file}（${ref} 中不存在）`);
        continue;
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
    } else if (stripParagraphs(before) === stripParagraphs(after)) {
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
