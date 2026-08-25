import {buildNodeId, destinations, rawJson} from "./model.mjs";

export const valleyIVRecycleBins = destinations.filter(
    (destination) => destination.kind === "recycle_bin" && destination.map === "map01",
);

function candidateNode(destination) {
    return `AutoDeliveryFindRecycleBin${buildNodeId(destination.id)}`;
}

export const recycleBinResolveNodes = valleyIVRecycleBins.map((destination) => ({
    node: candidateNode(destination),
    destinationId: destination.id,
}));

const destinationsByArea = new Map();
for (const destination of valleyIVRecycleBins) {
    const areaDestinations = destinationsByArea.get(destination.areaId) ?? [];
    areaDestinations.push(destination);
    destinationsByArea.set(destination.areaId, areaDestinations);
}
const rows = [];

function addNode(node, body) {
    rows.push({
        Node: node,
        NodeBody: rawJson(body),
    });
}

for (const [
    areaId,
    areaDestinations,
] of destinationsByArea) {
    if (areaDestinations.length !== 2) {
        throw new Error(
            `[AutoDelivery] 四号谷地区域 ${areaId} 的资源回收站数量应为 2，实际为 ${areaDestinations.length}`,
        );
    }

    const [sample] = areaDestinations;
    const areaName = sample.area.zh_cn;
    const viewMapNode = `AutoDeliveryViewRecycleBin${areaId}Map`;
    const startTrackingNode = `AutoDeliveryStartTrackingRecycleBin${areaId}`;
    const inMapNode = `AutoDeliveryRecycleBin${areaId}InDestinationMap`;
    const candidateNodes = areaDestinations.map(candidateNode);

    addNode(viewMapNode, {
        desc: `当前已追踪四号谷地-${areaName}资源回收站任务时，打开任务目标地图`,
        recognition: "And",
        all_of: [
            "TrackedMissionMapButton",
        ],
        box_index: 0,
        pre_delay: 0,
        action: "Click",
        post_delay: 0,
        rate_limit: 500,
        next: [
            inMapNode,
        ],
    });
    addNode(startTrackingNode, {
        desc: `开始追踪四号谷地-${areaName}资源回收站任务并打开任务目标地图`,
        recognition: "And",
        all_of: [
            "AutoDeliveryStartTrackingButton",
        ],
        box_index: 0,
        pre_delay: 0,
        action: "Click",
        post_delay: 0,
        rate_limit: 500,
        next: [
            inMapNode,
        ],
    });
    addNode(inMapNode, {
        desc: `确认已打开四号谷地-${areaName}资源回收站任务地图`,
        recognition: "TemplateMatch",
        roi: [
            900,
            10,
            130,
            140,
        ],
        template: "AutoDelivery/TrackTaskSuccess.png",
        pre_delay: 0,
        post_delay: 0,
        rate_limit: 0,
        next: candidateNodes,
    });

    for (const destination of areaDestinations) {
        addNode(candidateNode(destination), {
            desc: `在四号谷地-${areaName}大地图的指定坐标确认资源回收站图标（${destination.id}）`,
            recognition: "Custom",
            custom_recognition: "MapFind",
            custom_recognition_param: {
                zone: "ValleyIV",
                icon: "RecycleBin",
                at: destination.mapAt,
            },
            pre_delay: 0,
            action: "Custom",
            custom_action: "AutoDeliveryResolveDestinationAction",
            custom_action_param: {
                destination_id: destination.id,
            },
            post_delay: 0,
            rate_limit: 0,
            next: [
                "AutoDeliveryPrepareNavigateDestination",
            ],
            focus: {
                "Node.Action.Succeeded": `已通过地图图标确认资源回收站 ${destination.id}`,
            },
        });
    }
}

export default rows;
