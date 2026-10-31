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
            const recognitionParam = {
                grid_type: "transfer",
                item_ids: [itemId],
                item_recheck_filters: ["Normal:Usable"],
                deduplicate: true,
            };
            return {
                name: itemId,
                label: `$iconRecognition.name.${itemId}`,
                pipeline_override: Object.fromEntries(
                    [
                        `UseConsumableFindItemSlot${slot}`,
                        `UseConsumableStockFindItemSlot${slot}`,
                    ].map((node) => [
                        node,
                        {
                            recognition: {
                                param: {custom_recognition_param: recognitionParam},
                            },
                        },
                    ]),
                ),
            };
        }),
        default_case: defaultItemId,
    };
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

export default function buildUseConsumableTask({
    slots,
    defaultSlotCase,
    itemIds,
    defaultItemId,
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

    return {
        task: [
            {
                ...task,
                option: [
                    ...slots.map((slot) => `UseConsumableSlot${slot}`),
                    "UseConsumableRegion",
                ],
            },
        ],
        option: options,
    };
}
