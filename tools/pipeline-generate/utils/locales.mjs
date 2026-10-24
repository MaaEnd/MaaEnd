// assets/locales 下的翻译文件由 prettier 的 maafw-sort 插件（`/locales/.*\.jsonc?`）递归按
// 字母序排列对象键：UTF-16 code unit 顺序，与 `Array.prototype.sort()` 的默认比较一致。
//
// 生成器若按业务分组插入键，prettier 跑一遍就会把它们重排，于是每次生成都多出一份纯顺序 diff。
// 写回前先用这个函数排好，生成结果就等于格式化结果。

export function sortLocaleKeys(messages) {
    const sorted = {};
    for (const key of Object.keys(messages).sort()) {
        sorted[key] = messages[key];
    }
    return sorted;
}
