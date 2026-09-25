import {commissionMaps} from "./commission-data.mjs";
import {candidatesRows, commissionSourceOrder, endpointEntries} from "./endpoint-filter-data.mjs";
import {syncSeizeDeliveryJobsLocales} from "./sync-locales.mjs";

const mapNameOf = new Map(
    commissionMaps.map(({MapId, MapName}) => [
        MapId,
        MapName,
    ]),
);

function buildSourceCase({CaseId, MapId, Expected, FilterName}) {
    const mapName = mapNameOf.get(MapId);
    if (!mapName) {
        throw new Error(`[SeizeDeliveryJobs] 委托来源 ${CaseId} 引用了未登记的地区 ${MapId}`);
    }
    const filter = FilterName ?? mapName;
    return {
        name: CaseId,
        label: `$task.SeizeDeliveryJobsCommissionSource.cases.${CaseId}.label`,
        option: [`SeizeDeliveryJobsSpecifyDeliveryPoint${CaseId}`],
        pipeline_override: {
            __SeizeDeliveryJobsRecoOrigin: {expected: Expected},
            // override 是字段级替换，不是追加：被覆盖的 next 必须把原本要保留的节点一起写全。
            // 风险知悉拦截（Guard）由「全自动送货」档的后处理路径负责、已有委托由任务入口门控
            // （SeizeDeliveryJobsCheckOngoingJob）负责，因此都不在这两条 next 里。
            SeizeDeliveryJobsMain: {
                next: [`SeizeDeliveryJobsEnter${mapName}JobList`],
            },
            SeizeDeliveryJobsReadyToSeize: {
                next: [
                    "SeizeDeliveryJobsFailedChanceExhausted",
                    "SeizeDeliveryJobsPrepareAtDijiang",
                    `SeizeDeliveryJobsJobListIs${filter}Filter`,
                    `SeizeDeliveryJobsJobListSelect${filter}FilterPre`,
                ],
            },
            // 传送到帝江号后仍从本委托来源对应的仓储重新进入委托列表；
            // 与 Main 同理，字段级替换必须把 next 一起写全。
            SeizeDeliveryJobsPrepareAtDijiang: {
                next: [`SeizeDeliveryJobsEnter${mapName}JobList`],
            },
            // 帝江号传送后重新进列表时也要按来源收敛筛选分支：否则列表里残留的其它地图筛选
            // 会先把 SeizeDeliveryJobsJobListIs*Filter 命中，直接进 Loop 抢错地图的单。
            SeizeDeliveryJobsReadyAfterDijiangTeleport: {
                next: [
                    `SeizeDeliveryJobsJobListIs${filter}Filter`,
                    `SeizeDeliveryJobsJobListSelect${filter}FilterPre`,
                ],
            },
        },
    };
}

// 委托来源顺序（全部地区 → 新地区的全部 → 该地区各区域 → 旧地区…）由数据源推导，见 endpoint-filter-data.mjs。
// 旧区域顺序、新地区的位置都无需手工登记，新增地区 / 区域会随数据源自动排好。
const commissionSourceCases = commissionSourceOrder.map((descriptor) => buildSourceCase(descriptor));
const allDeliveryPointOptions = candidatesRows.map(({AreaId}) => `SeizeDeliveryJobsDeliveryPoint${AreaId}`);
// 以下是 task-template.jsonc 地区级选项块引用的占位符；新增地区除了照抄模板块，还要在这里补一行 <地区名>DeliveryPointOptions。
const deliveryPointOptionsOfMap = (MapId) =>
    candidatesRows.filter((row) => row.MapId === MapId).map(({AreaId}) => `SeizeDeliveryJobsDeliveryPoint${AreaId}`);
const wulingDeliveryPointOptions = deliveryPointOptionsOfMap("map02");
const valleyIVDeliveryPointOptions = deliveryPointOptionsOfMap("map01");

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
