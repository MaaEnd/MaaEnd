import * as multilineArrays from "prettier-plugin-multiline-arrays";
import * as maafwSort from "@nekosu/prettier-plugin-maafw-sort";

export default {
    plugins: [
        maafwSort.patchPlugin(multilineArrays),
    ],
    multilineArraysWrapThreshold: 1,
    maafwPipelinePatterns: [
        "/pipeline/.*\\.jsonc?",
        "/tools/pipeline-generate/(?!.*task-template\\.jsonc$).*\\.jsonc$",
    ],
    maafwInterfacePatterns: [
        "/interface\\.jsonc?",
        "/tasks/.*\\.jsonc?",
        "/tools/pipeline-generate/.*/task-template\\.jsonc?$",
    ],
    tabWidth: 4,
    printWidth: 120,
    useTabs: false,
    bracketSameLine: true,
    bracketSpacing: false,
    endOfLine: "auto",
    overrides: [
        {
            files: [
                "**/*.yml",
                "**/*.yaml",
            ],
            options: {
                parser: "yaml",
                tabWidth: 2,
            },
        },
        {
            files: [
                "*.json",
                "*.jsonc",
            ],
            options: {
                parser: "json",
                useTabs: false,
                bracketSameLine: false,
                trailingComma: "none",
            },
        },
        {
            files: [
                "*.mts",
                "**/*.ts",
            ],
            options: {
                tabWidth: 2,
                semi: false,
                trailingComma: "all",
                bracketSpacing: true,
                singleQuote: true,
                multilineArraysWrapThreshold: -1,
            },
        },
    ],
};
