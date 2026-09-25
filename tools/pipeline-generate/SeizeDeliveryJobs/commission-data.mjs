import {depots, mapSources} from "../AutoDelivery/model.mjs";

// 地区名与仓储文案节点需要手工登记：数据源只提供地图 zone 与仓储 / 终点名称，没有地区显示名。
const mapNames = {
    map01: "ValleyIV",
    map02: "Wuling",
};
const mapLabels = {
    map01: {zh_cn: "四号谷地", zh_tw: "四號谷地", en_us: "Valley IV", ja_jp: "四号谷地", ko_kr: "제4 계곡"},
    map02: {zh_cn: "武陵", zh_tw: "武陵", en_us: "Wuling", ja_jp: "武陵", ko_kr: "무릉"},
};
const depotTextNodes = {
    map01: "OriginiumScienceParkText",
    map02: "WulingCityText",
};

export const commissionMaps = [
    ...new Map(
        depots.map((depot) => [
            depot.map,
            {
                MapId: depot.map,
                MapName: mapNames[depot.map],
                AreaName: mapLabels[depot.map].zh_cn,
                DepotTextNode: depotTextNodes[depot.map],
                Labels: mapLabels[depot.map],
            },
        ]),
    ).values(),
];
export default commissionMaps;

// 展示顺序「新地区优先」：zone_id 越大表示地图加入游戏越晚（四号谷地 1 → 武陵 2），
// 新增地区只要数据源出现新地图就会自动排到最前，无需在生成器里手工登记顺序。
// 数据源缺少 zone_id 时退回地图 ID 倒序，保证顺序仍然确定。
export const commissionMapsNewestFirst = [
    ...commissionMaps,
].sort((left, right) => {
    const byZone = (mapSources[right.MapId]?.zone_id ?? 0) - (mapSources[left.MapId]?.zone_id ?? 0);
    return byZone !== 0 ? byZone : right.MapId.localeCompare(left.MapId);
});

// 兼容历史用户配置：武陵的地区级选项沿用旧名 Unlimited，其余地区按 <地区名>Unlimited 命名。
export const mapWideCaseId = (MapName) => (MapName === "Wuling" ? "Unlimited" : `${MapName}Unlimited`);
