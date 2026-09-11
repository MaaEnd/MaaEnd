import {buildNodeId, destinations} from "../AutoDelivery/model.mjs";
import {resolveEndpointNames, syncEndpointLabels} from "./endpoint-labels.mjs";

const endpointLabels = syncEndpointLabels(destinations);

// MapFind 认的是点击「查看位置」后游戏在送货终点坐标上绘制的那个标记图标，
// 全部终点共用同一个图标、靠 at 坐标区分（每个终点的底图坐标各不相同）。
// 图标条目 DeliveryPoint 登记在 assets/resource/image/SceneManager/MapIcons.json，
// 模板图为 assets/resource/image/SeizeDeliveryJobs/DeliveryPoint.png（阈值待游戏内微调）。
const ENDPOINT_ICON = "DeliveryPoint";

// 保留 PR 中已有终点的节点 / task case ID 和候选顺序，兼容已保存的用户选项。
// 此表仅用于兼容命名，不限制终点范围；其余终点从 AutoDelivery 源 ID 自动派生 PascalCase ID。
// 展示名称由 endpoint-labels.json 维护，留空时使用目录中的收货人名称。
const LEGACY_ENDPOINTS = [
    {
        endpoint: "Owl",
        destinationId: "deliver_target_map02_lv002_recycle_01",
    },
    {
        endpoint: "MaterialResearchInstitute",
        destinationId: "deliver_target_map02_lv002_02",
    },
    {
        endpoint: "Observatory",
        destinationId: "deliver_target_map02_lv002_03",
    },
    {
        endpoint: "TechProductionOffice",
        destinationId: "deliver_target_map02_lv002_01",
    },
    {
        endpoint: "No1TypeCAnchorArea",
        destinationId: "deliver_target_map02_lv005_02",
    },
    {
        endpoint: "No3TypeCAnchorArea",
        destinationId: "deliver_target_map02_lv005_03",
    },
    {
        endpoint: "JingweiFieldArea",
        destinationId: "deliver_target_map02_lv005_01",
    },
];

const legacyById = new Map(
    LEGACY_ENDPOINTS.map((entry) => [
        entry.destinationId,
        entry,
    ]),
);
const legacyOrder = new Map(
    LEGACY_ENDPOINTS.map((entry, index) => [
        entry.destinationId,
        index,
    ]),
);

// 遍历 AutoDelivery 完整目录，坐标、地图区域与多语言名称均来自同一数据源。
// 旧终点维持原顺序，其余终点保持 model.mjs 中按源 ID 排序的顺序；删除的终点不会残留。
export const endpointEntries = [...destinations]
    .sort(
        (left, right) =>
            (legacyOrder.get(left.id) ?? LEGACY_ENDPOINTS.length) -
            (legacyOrder.get(right.id) ?? LEGACY_ENDPOINTS.length),
    )
    .map((destination) => {
        const legacy = legacyById.get(destination.id);
        const endpoint = legacy?.endpoint ?? buildNodeId(destination.id);
        const names = resolveEndpointNames(destination, endpointLabels);
        return {
            EndpointId: endpoint,
            DestinationId: destination.id,
            Names: names,
            Desc: `「${names.zh_cn}」送货终点（${destination.id}）：candidates 候选开关，命中后前往接取`,
            AreaId: destination.areaId,
            MapId: destination.map,
            AreaName: destination.area.zh_cn,
            AreaTexts: destination.area,
            MapZone: destination.mapZone,
            DestinationMapAt: destination.mapAt,
        };
    });

if (new Set(endpointEntries.map((entry) => entry.EndpointId)).size !== endpointEntries.length) {
    throw new Error("[SeizeDeliveryJobs] 终点节点 ID 重复，请检查兼容命名与 AutoDelivery 源 ID");
}

// 叶子节点：candidates 每个候选的开关兼命中落点。enabled 默认关，由 task 选项逐个打开；
// 关着的候选在 MapFind 里连认都不认、直接跳过。节点本身不再做识别，命中后直接前往接取。
export const endpointFilterRows = endpointEntries.map(({EndpointId, Desc}) => ({
    EndpointId,
    Desc,
}));

export const endpointNodeNames = endpointEntries.map((row) => `SeizeDeliveryJobsEndpointFilter${row.EndpointId}`);

// 按区域分组：每个区域生成一个 candidates 节点，节点名 SeizeDeliveryJobsEndpointCandidates{AreaId}。
// 终点区域 == 委托出发地（取货仓储）区域（AutoDelivery 目录强制校验区域↔仓储 1:1），且点「查看位置」后
// 地图以终点为中心打开——所以运行时按出发地只路由到对应区域节点，本区域候选基本落在屏内，无需跨区域来回拖动。
// 分组保持 endpointEntries 的顺序（区域内候选顺序、区域间先后顺序）。
const areaOrder = [];
const entriesByArea = new Map();
for (const entry of endpointEntries) {
    if (!entriesByArea.has(entry.AreaId)) {
        entriesByArea.set(entry.AreaId, []);
        areaOrder.push(entry.AreaId);
    }
    entriesByArea.get(entry.AreaId).push(entry);
}

// 每个区域一个 candidates 节点（多行）：区域内候选共享一次缩放与视口求解。
// 一个 MapFind 节点只有一个 zone，故同区域候选必须同 zone；跨 zone 直接报错，避免默默生成认不对的节点。
export const candidatesRows = areaOrder.map((areaId) => {
    const entries = entriesByArea.get(areaId);
    const zones = [
        ...new Set(entries.map((entry) => entry.MapZone)),
    ];
    if (zones.length !== 1) {
        throw new Error(
            `[SeizeDeliveryJobs] 区域 ${areaId} 的 candidates 需同 zone，当前有 ${zones.join(", ")}；请为不同 zone 各起一个 candidates 节点`,
        );
    }
    return {
        AreaId: areaId,
        AreaName: entries[0].AreaName,
        Zone: zones[0],
        Icon: ENDPOINT_ICON,
        Candidates: entries.map((entry) => ({
            at: entry.DestinationMapAt,
            next: `SeizeDeliveryJobsEndpointFilter${entry.EndpointId}`,
        })),
        Expected: [
            ...new Set(entries.flatMap((entry) => Object.values(entry.AreaTexts))),
        ],
    };
});

// 守卫节点数据（单行）：next 只列出全部区域门控节点。
// 框架对 next 逐轮识别、首个命中胜出：标题未加载完整时继续识别，匹配的门控 hit 进对应 candidates。
export const dispatcherRows = [
    {
        NextList: [
            ...areaOrder.map((areaId) => `SeizeDeliveryJobsEndpointRegion${areaId}`),
        ],
    },
];

export default endpointFilterRows;
