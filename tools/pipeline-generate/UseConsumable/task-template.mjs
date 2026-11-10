function buildSlotOverride(slot, enabled) {
    return {
        [`UseConsumableSlot${slot}Enabled`]: {enabled},
        [`UseConsumableSlot${slot}Disabled`]: {enabled: !enabled},
        [`UseConsumablePrecheckStartSlot${slot}`]: {enabled},
        [`UseConsumableNeedStockSlot${slot}`]: {enabled},
        [`UseConsumableStockFindItemSlot${slot}`]: {enabled},
        [`UseConsumableUseStartSlot${slot}`]: {enabled},
        [`UseConsumableNeedValleyIVSlot${slot}`]: {enabled: false},
    };
}

function buildSlotOption(slot, defaultCase) {
    return {
        type: "switch",
        label: `$option.UseConsumableOperators.cases.OperatorSlot${slot}.label`,
        cases: [
            {
                name: "Yes",
                option: [`UseConsumableSlot${slot}Item`],
                pipeline_override: buildSlotOverride(slot, true),
            },
            {
                name: "No",
                pipeline_override: buildSlotOverride(slot, false),
            },
        ],
        default_case: defaultCase,
    };
}

function buildItemOption(slot, itemIds, defaultItemId) {
    return {
        type: "select",
        label: "$option.UseConsumableItem.label",
        cases: itemIds.map((itemId) => {
            return {
                name: itemId,
                label: `$iconRecognition.name.${itemId}`,
                pipeline_override: buildItemRecognitionOverrides(
                    [
                        `UseConsumableFindItemSlot${slot}`,
                        `UseConsumableStockFindItemSlot${slot}`,
                    ],
                    itemId,
                ),
            };
        }),
        default_case: defaultItemId,
    };
}

function buildItemRecognitionOverrides(nodes, itemId) {
    const recognitionParam = {
        grid_type: "transfer",
        item_ids: [itemId],
        item_recheck_filters: ["Normal:Usable"],
        deduplicate: true,
    };
    return Object.fromEntries(
        nodes.map((node) => [
            node,
            {
                recognition: {
                    param: {custom_recognition_param: recognitionParam},
                },
            },
        ]),
    );
}

function buildRegionOption(regions, defaultRegion) {
    return {
        type: "select",
        label: "$option.UseConsumableRegion.label",
        cases: regions.map((region) => ({
            name: region.id,
            label: `$option.UseConsumableRegion.cases.${region.id}.label`,
            pipeline_override: {
                UseConsumableUseTeleportToRegion: {
                    custom_action_param: {
                        sub: [region.scene],
                        continue: false,
                        strict: true,
                    },
                },
            },
        })),
        default_case: defaultRegion,
    };
}

function buildStashBackpackOption() {
    return {
        type: "switch",
        label: "$task.StashBackpack.option.StashBackpackSubTask.label",
        cases: [
            {
                name: "Yes",
                option: [
                    "StashBackpackType",
                    "StashBackpackArea",
                ],
                pipeline_override: {StashBackpackMain: {enabled: true}},
            },
            {
                name: "No",
                pipeline_override: {StashBackpackMain: {enabled: false}},
            },
        ],
        default_case: "Yes",
    };
}

function buildTacticalItemsOption() {
    return {
        type: "switch",
        label: "$option.UseConsumableTacticalItems.label",
        cases: [
            {
                name: "Yes",
                option: ["UseConsumableTacticalItem"],
                pipeline_override: {UseConsumableTacticalItemsEnabled: {enabled: true}},
            },
            {
                name: "No",
                pipeline_override: {UseConsumableTacticalItemsEnabled: {enabled: false}},
            },
        ],
        default_case: "No",
    };
}

function buildTacticalItemOption(tacticalItemIds) {
    return {
        type: "select",
        label: "$option.UseConsumableTacticalItem.label",
        cases: tacticalItemIds.map((itemId) => {
            return {
                name: itemId,
                label: `$iconRecognition.name.${itemId}`,
                pipeline_override: buildItemRecognitionOverrides(
                    [
                        "UseConsumableTacticalFindBackpackItem",
                        "UseConsumableTacticalFindDepotItem",
                    ],
                    itemId,
                ),
            };
        }),
        default_case: tacticalItemIds[0],
    };
}

function buildUseConsumableEnabledOption(slots) {
    return {
        type: "switch",
        label: "$option.UseConsumableEnabled.label",
        cases: [
            {
                name: "Yes",
                option: [
                    ...slots.map((slot) => `UseConsumableSlot${slot}`),
                    "UseConsumableRegion",
                ],
                pipeline_override: {UseConsumableTaskConsumablesEnabled: {enabled: true}},
            },
            {
                name: "No",
                pipeline_override: {UseConsumableTaskConsumablesEnabled: {enabled: false}},
            },
        ],
        default_case: "Yes",
    };
}

export default function buildUseConsumableTask({
    slots,
    defaultSlotCase,
    itemIds,
    defaultItemId,
    tacticalItemIds,
    regions,
    defaultRegion,
    ...task
}) {
    const options = {};
    for (const slot of slots) {
        options[`UseConsumableSlot${slot}`] = buildSlotOption(slot, defaultSlotCase);
        options[`UseConsumableSlot${slot}Item`] = buildItemOption(slot, itemIds, defaultItemId);
    }
    options.UseConsumableRegion = buildRegionOption(regions, defaultRegion);
    options.UseConsumableStashBackpackSubTask = buildStashBackpackOption();
    options.UseConsumableTacticalItems = buildTacticalItemsOption();
    options.UseConsumableTacticalItem = buildTacticalItemOption(tacticalItemIds);
    options.UseConsumableEnabled = buildUseConsumableEnabledOption(slots);

    return {
        task: [
            {
                ...task,
                option: [
                    "UseConsumableStashBackpackSubTask",
                    "UseConsumableTacticalItems",
                    "UseConsumableEnabled",
                ],
            },
        ],
        option: options,
    };
}
