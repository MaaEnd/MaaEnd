# Native cultivation prototype

Updated: 2026-10-20.

This prototype adds native MaaEnd tasks for reading cultivation needs, previewing them, exporting goal previews, and clearing the plan. It targets `v2.35.0-beta.1` and has no runtime dependency on SRC, Python, or PyWebIO. Compilation, offline rules, and resource validation passed; device acceptance on the new version is pending.

## What this version provides

- Reads the selected operator and expectation across four tabs, twice, in Simplified Chinese at 1280×720 through CloudADB.
- Confirms the selected tab, list boundaries, material IDs, and quantities; replaces the plan only after consistent complete passes, preserving the previous plan on failure.
- Shows required, owned, and missing amounts; converts farmable items into all 16 SupplyPlan total-stock goals and exports a parameter preview.
- Records the weapon name and candidate internal IDs; essence requirements remain separate from protocol-space material goals.
- Clears the plan and invalidates writes from an older scan.

## User steps

1. Launch an isolated trial copy of MaaEnd, select the cloud ADB controller, and connect to a device already inside the game. Keep the prototype separate from the existing installation.
2. Open `培养总览` (Cultivation Overview) in the game, select an operator, and set `培养期望` (Cultivation Expectation). Keep `显示换算结果` (Show Conversion Results) consistent with the preserved material-page samples.
3. Run only `读取培养需求（原生试作）` (Read cultivation needs, native prototype). The tool switches tabs and scrolls; it does not click acquisition, upgrade, box-opening, or reward controls.
4. Inspect the result, or run `预览培养需求` (Preview cultivation needs). This version stores one operator plan; a successful new scan replaces it.
5. Run `生成刷取目标预览` (Export farming goal preview). The file is `debug/record/CultivationSupplyPlan.preview.json`. Its `goals` are total stock targets and its `pipeline_override` uses the upstream option template. A required amount of 80 with 30 owned means a target of 80 and a deficit of 50.
6. Run `清空培养需求` (Clear cultivation needs) to remove the plan.

**This prototype ends at goal preview. It neither starts farming nor updates and persists MXU's existing ProtocolSpace form.** The export carries `executable=false`. Actual farming requires device validation of the native scan, execution-contract review, and reconciliation of the existing consumption state. Removing the marker does not make the preview executable.

## Weapons and essences

The upstream catalogue can assign multiple internal IDs to one displayed weapon name. When the name alone cannot distinguish them, the plan retains `candidate_ids` rather than selecting an identity.

The latest beta still restricts AutoEssence controllers. After that beta was published, upstream merged [#6449](https://github.com/MaaEnd/MaaEnd/pull/6449) to expose the task, while explicitly leaving actual ADB recognition and operation adaptation for later changes. Recording a weapon does not prove its essence meets the cultivation target; skills, quantity, and quality criteria require separate confirmation.

## Failures and limitations

An unexpected screen, ambiguous item, inconsistent inventory, disagreeing passes, or unconfirmed list end fails the task while retaining the previous plan. An older preview after failure is not a successful new observation.

Coordinates were ported from preserved screenshots; the new beta's single-ROI icon-recognition path requires device validation. Unrecognized materials are not treated as zero or complete. Existing logs and state remain separate; SRC account and runtime state are not imported.

## Trial bundle

The delivered `overlay` contains replacement files, not a full MaaEnd distribution. Extract the official Windows x86-64 `v2.35.0-beta.1` release into a new directory, close that trial client, and copy the overlay contents there. Do not mix versions. This work did not install, launch, or connect the trial copy on the user's behalf.

`go-service.exe` is the rebuilt complete native Agent. JSON files alone do not register the new components, and this Agent must not be substituted into another version. An automatic update may overwrite prototype files; recheck the version and manifest before further use.
