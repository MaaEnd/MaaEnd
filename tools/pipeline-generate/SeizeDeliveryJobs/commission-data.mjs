import {depots} from "../AutoDelivery/model.mjs";

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
