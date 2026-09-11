import {candidatesRows, endpointEntries} from "./endpoint-filter-data.mjs";
import {syncSeizeDeliveryJobsLocales} from "./sync-locales.mjs";

function buildSourceCase(areaId, expected, mapId, filterName) {
    // 当前仅有两个大地区，若之后新增需要对下面 mapId 判定进行修改
    // map01 == ValleyIV
    // map02 == Wuling
    const mapName = mapId === "map01" ? "ValleyIV" : "Wuling";
    const filter = filterName ?? mapName;
    return {
        name: areaId,
        label: `$task.SeizeDeliveryJobsCommissionSource.cases.${areaId}.label`,
        option: [`SeizeDeliveryJobsSpecifyDeliveryPoint${areaId}`],
        pipeline_override: {
            __SeizeDeliveryJobsRecoOrigin: {expected},
            // override 是字段级替换，不是追加：被覆盖的 next 必须把原本要保留的节点一起写全，
            // 否则风险知悉拦截（Guard）和已有委托 / 今日上限检查都会失效。
            SeizeDeliveryJobsMain: {
                next: [
                    "SeizeDeliveryJobsGuard",
                    `SeizeDeliveryJobsEnter${mapName}JobList`,
                ],
            },
            SeizeDeliveryJobsReadyToSeize: {
                next: [
                    "SeizeDeliveryJobsExistDueTask",
                    "SeizeDeliveryJobsFailedChanceExhausted",
                    `SeizeDeliveryJobsJobListIs${filter}Filter`,
                    `SeizeDeliveryJobsJobListSelect${filter}FilterPre`,
                ],
            },
        },
    };
}

const areaSourceCases = candidatesRows.map(({AreaId, Expected}) => {
    const mapId = endpointEntries.find((entry) => entry.AreaId === AreaId).MapId;
    return {MapId: mapId, Case: buildSourceCase(AreaId, Expected, mapId)};
});
// 旧区域名只用于保持既有选项顺序，新区域按数据源顺序排在旧区域之后。
// 因此新增区域无需手工登记，来源 case 会随所属地图自动生成。
const LEGACY_AREA_ORDER = [
    "WulingCity",
    "TestArea",
    "OriginiumSciencePark",
    "OriginLodespring",
    "PowerPlateau",
];
const areaCasesOfMap = (mapId) =>
    areaSourceCases
        .filter((entry) => entry.MapId === mapId)
        .map(({Case}) => Case)
        .sort((left, right) => {
            const leftIndex = LEGACY_AREA_ORDER.indexOf(left.name);
            const rightIndex = LEGACY_AREA_ORDER.indexOf(right.name);
            if (leftIndex === -1 && rightIndex === -1) return 0;
            if (leftIndex === -1) return 1;
            if (rightIndex === -1) return -1;
            return leftIndex - rightIndex;
        });
// 为避免原用户配置丢失，Unlimited 视作 WulingUnlimited
// 后续若出现新地区应按 地区名+Unlimited 命名
const expectedOfMap = (mapId) => [
    ...new Set(
        candidatesRows
            .filter(({AreaId}) => endpointEntries.some((entry) => entry.AreaId === AreaId && entry.MapId === mapId))
            .flatMap(({Expected}) => Expected),
    ),
];
const commissionSourceCases = [
    buildSourceCase("AllUnlimited", [...new Set(candidatesRows.flatMap(({Expected}) => Expected))], "map02", "All"),
    buildSourceCase("Unlimited", expectedOfMap("map02"), "map02"),
    ...areaCasesOfMap("map02"),
    buildSourceCase("ValleyIVUnlimited", expectedOfMap("map01"), "map01"),
    ...areaCasesOfMap("map01"),
];
const allDeliveryPointOptions = candidatesRows.map(({AreaId}) => `SeizeDeliveryJobsDeliveryPoint${AreaId}`);
const wulingDeliveryPointOptions = candidatesRows
    .filter(({AreaId}) => endpointEntries.some((entry) => entry.AreaId === AreaId && entry.MapId === "map02"))
    .map(({AreaId}) => `SeizeDeliveryJobsDeliveryPoint${AreaId}`);
const valleyIVDeliveryPointOptions = candidatesRows
    .filter(({AreaId}) => endpointEntries.some((entry) => entry.AreaId === AreaId && entry.MapId === "map01"))
    .map(({AreaId}) => `SeizeDeliveryJobsDeliveryPoint${AreaId}`);

export const taskRows = candidatesRows.map(({AreaId}) => {
    const entries = endpointEntries.filter((entry) => entry.AreaId === AreaId);
    return {
        AreaId,
        CommissionSourceCases: commissionSourceCases,
        AllDeliveryPointOptions: allDeliveryPointOptions,
        WulingDeliveryPointOptions: wulingDeliveryPointOptions,
        ValleyIVDeliveryPointOptions: valleyIVDeliveryPointOptions,
        EndpointIds: entries.map(({EndpointId}) => EndpointId),
        DeliveryPointCases: entries.map(({EndpointId}) => ({
            name: EndpointId,
            label: `$task.SeizeDeliveryJobsDeliveryPoint${AreaId}.cases.${EndpointId}.label`,
            pipeline_override: {
                [`SeizeDeliveryJobsEndpointFilter${EndpointId}`]: {enabled: true},
            },
        })),
    };
});

// run-all 直接载入 task 数据时一并补齐文案，新增区域 / 终点无需额外运行同步命令。
syncSeizeDeliveryJobsLocales();

export default taskRows;
