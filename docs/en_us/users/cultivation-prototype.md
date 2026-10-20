# Native cultivation prototype

Updated: 2026-10-20. Prototype.2 targets `v2.35.0-beta.1` and has no SRC, Python or PyWebIO runtime dependency.

## User workflow

1. Prepare an isolated official Windows x86-64 beta copy and apply the matching overlay, including the full Agent. Preserve the production installation.
2. Open the cultivation overview in game, select an operator and expectation, and keep the conversion display setting consistent with the samples.
3. Run only the native cultivation read task. It scans four tabs twice and retains the previous plan on failure. It does not obtain resources, upgrade, open boxes or claim rewards.
4. Open cultivation task progress for required/owned/missing quantities, assigned module, scan time and revision. One operator plan is retained; a new scan replaces it.
5. Export the module integration preview to `debug/record/CultivationModules.preview.json`.
6. After personally completing development in game, run the dedicated manual confirmation task separately. Do not put that task into an automatic routine.
7. A new scan clears the old completion confirmation. Clearing the plan also invalidates pending writes.

**This version converts and displays plans; it does not dispatch farming or persist existing MXU task forms.** Module previews retain `executable=false` and explicit blockers. Removing that flag does not authorize execution. Dispatch, consumption contracts, device acceptance and formal review remain pending.

## Progress and completion

The view distinguishes missing materials, materials ready at the recorded scan time, and user-confirmed completion. User confirmation includes its source, operator, expectation, revision and timestamp. Automatic verification of actual upgrades, promotions, skills and equipment is not implemented. Module success, termination or reward claiming never supplies that evidence. Experience denominations are pooled into equivalent experience.

Quantities are historical observations. Recheck after running modules. Old IMS caches without account and freshness guarantees cannot automatically turn progress green. After manual confirmation, material counts remain historical references.

## Module adapters

Protocol Space uses the upstream 16 total-stock goals, with unrequested goals explicitly zero. A requirement of80 with30 owned still produces a goal of80. The preview adds a positive inventory check to completion.

Collection maps25 existing routes to26 items, including a shared two-item route. Unneeded routes are disabled explicitly: upstream zero means unlimited. The cultivation preview stops on warehouse sync failure instead of falling back to unconditional collection. Upstream runs each selected route once; route completion does not prove enough materials were gathered.

Essence uses existing weapon target mode. Ambiguous weapon IDs remain unselected. The upstream target checks for at least one matching trait combination, ignoring trait levels and equipment. Cloud adaptation and quality requirements need separate validation.

CloudADB is excluded from Collection target-stock mode and Essence in the fixed beta. Upstream [PR#6449](https://github.com/MaaEnd/MaaEnd/pull/6449) opens the Essence entry after that beta, but does not prove cloud operation. Exporting parameters does not solve these restrictions.

The eventual execution workflow is read, review, run supported modules, recheck shortages, develop in game, then manually confirm or automatically verify results. This prototype is not that full execution release.

## Boundaries

Keep the overlay paired with the exact official version. The Agent and resources must be updated together. Updates may overwrite prototype files. Only Simplified Chinese, one account and CloudADB are in scope; clear and rescan after switching accounts.

Luna's simulator evidence binds to the actually tested prototype.1 package. Prototype.2 integration previews, manual confirmation and the consumption chain require separate acceptance. Compilation and offline tests do not establish device or formal acceptance.
