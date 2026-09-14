import {buildNodeId, destinations} from "../AutoDelivery/model.mjs";
import {commissionMapsNewestFirst, mapWideCaseId} from "./commission-data.mjs";
import {resolveEndpointNames, syncEndpointLabels} from "./endpoint-labels.mjs";

const endpointLabels = syncEndpointLabels(destinations);

// MapFind 认的是点击「查看位置」后游戏在送货终点坐标上绘制的那个标记图标，
// 全部终点共用同一个图标、靠 at 坐标区分（每个终点的底图坐标各不相同）。
// 图标条目 DeliveryPoint 登记在 assets/resource/image/SceneManager/MapIcons.json，
// 模板图为 assets/resource/image/SeizeDeliveryJobs/DeliveryPoint.png（阈值待游戏内微调）。
const ENDPOINT_ICON = "DeliveryPoint";

// 保留 PR 中已有终点的节点 / task case ID 和候选顺序，兼容已保存的用户选项。
// 此表仅用于兼容命名，不限制终点范围；其余终点从 AutoDelivery 源 ID 自动派生 PascalCase ID。
// 展示名称优先取 endpoint-labels.json 登记的地点名（终点 NPC 所在的地点），其次用目录中的收货人名称；
// 方位同样登记在 endpoint-labels.json，由人工维护。
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
// 区域顺序：新地区优先，同一地区内保持数据源顺序；该顺序同时决定 task 的 cases 与文案键序，
// 新增区域自动落在所属地区末尾，无需再维护手工顺序表。
const areaSourceIndex = new Map();
for (const [
    index,
    destination,
] of destinations.entries()) {
    if (areaSourceIndex.has(destination.areaId)) continue;
    areaSourceIndex.set(destination.areaId, {
        AreaId: destination.areaId,
        MapId: destination.map,
        Index: index,
    });
}
const mapOrder = new Map(
    commissionMapsNewestFirst.map(({MapId}, index) => [
        MapId,
        index,
    ]),
);
const areaOrder = [
    ...areaSourceIndex.values(),
]
    .sort(
        (left, right) =>
            (mapOrder.get(left.MapId) ?? mapOrder.size) - (mapOrder.get(right.MapId) ?? mapOrder.size) ||
            left.Index - right.Index,
    )
    .map(({AreaId}) => AreaId);
const entriesByArea = new Map(
    areaOrder.map((areaId) => [
        areaId,
        [],
    ]),
);
for (const entry of endpointEntries) {
    const entries = entriesByArea.get(entry.AreaId);
    if (!entries) {
        throw new Error(`[SeizeDeliveryJobs] 终点 ${entry.EndpointId} 的区域 ${entry.AreaId} 不在数据源区域顺序中`);
    }
    entries.push(entry);
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
        MapId: entries[0].MapId,
        AreaName: entries[0].AreaName,
        AreaTexts: entries[0].AreaTexts,
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

// 委托来源候选（task 的 cases）顺序：全部地区 → 每个地区（新地区优先）的「该地区全部」→ 该地区的各区域。
// task 选项、locale 文案键序都取自这里，避免两处顺序各自漂移。
// 「全部地区」用新地区的仓储列表进入、用 All 筛选覆盖所有地区。
export const commissionSourceOrder = [
    {
        CaseId: "AllUnlimited",
        MapId: commissionMapsNewestFirst[0].MapId,
        FilterName: "All",
        Expected: [
            ...new Set(candidatesRows.flatMap(({Expected}) => Expected)),
        ],
    },
    ...commissionMapsNewestFirst.flatMap(({MapId, MapName}) => {
        const rows = candidatesRows.filter((row) => row.MapId === MapId);
        return [
            {
                CaseId: mapWideCaseId(MapName),
                MapId,
                FilterName: undefined,
                Expected: [
                    ...new Set(rows.flatMap(({Expected}) => Expected)),
                ],
            },
            ...rows.map(({AreaId, Expected}) => ({
                CaseId: AreaId,
                MapId,
                FilterName: undefined,
                Expected,
            })),
        ];
    }),
];

// 守卫节点数据（单行）：next 列出全部区域门控节点 + NotMatched 兜底。
// 框架对 next 逐个识别、首个命中胜出：当前子区域不匹配的门控 OCR miss，匹配的门控 hit 进对应 candidates。
export const dispatcherRows = [
    {
        NextList: [
            ...areaOrder.map((areaId) => `SeizeDeliveryJobsEndpointRegion${areaId}`),
            "SeizeDeliveryJobsEndpointNotMatched",
        ],
    },
];

export default endpointFilterRows;
