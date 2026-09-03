import {DELIVERY_JOB_FILL_ITEM_PRIORITY_COUNT, deliveryJobRegions, rawJson} from "./model.mjs";

export default deliveryJobRegions.flatMap((region) => {
    const defaultItem = region.FillItems.find((item) => item.Id === region.DefaultFillItem);
    if (!defaultItem) {
        throw new Error(`[DeliveryJobs] 地区 ${region.Id} 缺少默认装箱物品 ${region.DefaultFillItem}`);
    }

    return Array.from({length: DELIVERY_JOB_FILL_ITEM_PRIORITY_COUNT}, (_, index) => {
        const priority = index + 1;
        return {
            RegionId: region.Id,
            RegionName: region.Name,
            Priority: priority,
            Enabled: rawJson(priority === 1),
            DefaultFillItem: defaultItem.ItemId,
            DefaultFillItemRecheckFilter: defaultItem.RecheckFilter,
            NextPriorities: rawJson([
                ...Array.from(
                    {length: DELIVERY_JOB_FILL_ITEM_PRIORITY_COUNT - priority},
                    (_, nextIndex) => `DeliveryJobsStartFill${region.Id}Priority${priority + nextIndex + 1}`,
                ),
                "DeliveryJobsConfiguredFillItemsInsufficient",
            ]),
        };
    });
});
