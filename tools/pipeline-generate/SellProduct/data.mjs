// SellProduct 数据源
// 定义各售卖点的物品列表和多语言 OCR 识别文本

// ===== 物品定义 =====
// 每个物品: { id, name(简中), label(i18n key), expected(多语言 OCR 识别) }
const ITEMS = {
    BuckCapsuleA: {
        name: "精选荞愈胶囊",
        label: "$item.BuckCapsuleA",
        expected: [
            "^精選蕎癒膠囊$",
            "^精选荞愈胶囊$",
            "^蕎花カプセルⅢ$",
            "^메밀꽃 치유 캡슐(대)$",
            "^Buck Capsule [A]$",
        ],
    },
    HCValleyBattery: {
        name: "高容谷地电池",
        label: "$item.HCValleyBattery",
        expected: [
            "^高容量谷地電池$",
            "^高容谷地电池$",
            "^大容量谷地バッテリー$",
            "^대용량 협곡 배터리$",
            "^HC Valley Battery$",
        ],
    },
    CannedCitromeA: {
        name: "精选柑实罐头",
        label: "$item.CannedCitromeA",
        expected: [
            "^精選柑實罐頭$",
            "^精选柑实罐头$",
            "^シトロームの缶詰Ⅲ$",
            "^시트론 통조림(대)$",
            "^Canned Citrome [A]$",
        ],
    },
    SCValleyBattery: {
        name: "中容谷地电池",
        label: "$item.SCValleyBattery",
        expected: [
            "^中容量谷地電池$",
            "^中容谷地电池$",
            "^中容量谷地バッテリー$",
            "^중용량 협곡 배터리$",
            "^SC Valley Battery$",
        ],
    },
    CannedCitromeB: {
        name: "优质柑实罐头",
        label: "$item.CannedCitromeB",
        expected: [
            "^優質柑實罐頭$",
            "^优质柑实罐头$",
            "^シトロームの缶詰Ⅱ$",
            "^시트론 통조림(중)$",
            "^Canned Citrome [B]$",
        ],
    },
    BuckCapsuleB: {
        name: "优质荞愈胶囊",
        label: "$item.BuckCapsuleB",
        expected: [
            "^優質蕎癒膠囊$",
            "^优质荞愈胶囊$",
            "^蕎花カプセルⅡ$",
            "^메밀꽃 치유 캡슐(중)$",
            "^Buck Capsule [B]$",
        ],
    },
    BuckCapsuleC: {
        name: "荞愈胶囊",
        label: "$item.BuckCapsuleC",
        expected: [
            "^蕎癒膠囊$",
            "^荞愈胶囊$",
            "^蕎花カプセルⅠ$",
            "^메밀꽃 치유 캡슐$",
            "^Buck Capsule [C]$",
        ],
    },
    CannedCitromeC: {
        name: "柑实罐头",
        label: "$item.CannedCitromeC",
        expected: [
            "^柑實罐頭$",
            "^柑实罐头$",
            "^シトロームの缶詰Ⅰ$",
            "^시트론 통조림$",
            "^Canned Citrome [C]$",
        ],
    },
    AmethystBottle: {
        name: "紫晶质瓶",
        label: "$item.AmethystBottle",
        expected: [
            "^紫晶質瓶$",
            "^紫晶质瓶$",
            "^紫晶製ボトル$",
            "^자수정 병$",
            "^Amethyst Bottle$",
        ],
    },
    Origocrust: {
        name: "晶体外壳",
        label: "$item.Origocrust",
        expected: [
            "^晶體外殼$",
            "^晶体外壳$",
            "^結晶外殻$",
            "^오리고 크러스트$",
            "^Origocrust$",
        ],
    },
    AmethystPart: {
        name: "紫晶零件",
        label: "$item.AmethystPart",
        expected: [
            "^紫晶零件$",
            "^紫晶零件$",
            "^紫晶部品$",
            "^자수정 부품$",
            "^Amethyst Part$",
        ],
    },
    LCValleyBattery: {
        name: "低容谷地电池",
        label: "$item.LCValleyBattery",
        expected: [
            "^低容量谷地電池$",
            "^低容谷地电池$",
            "^小容量谷地バッテリー$",
            "^저용량 협곡 배터리$",
            "^LC Valley Battery$",
        ],
    },
    FerriumPart: {
        name: "铁制零件",
        label: "$item.FerriumPart",
        expected: [
            "^鐵製零件$",
            "^铁制零件$",
            "^鉄製部品$",
            "^페리움 부품$",
            "^Ferrium Part$",
        ],
    },
    SCWulingBattery: {
        name: "中容武陵电池",
        label: "$item.SCWulingBattery",
        expected: [
            "^中容武陵电池$",
            "^中容量武陵電池$",
            "^SC Wuling Battery$",
            "^中容量武陵バッテリー$",
            "^중용량 무릉 배터리$",
        ],
    },
    YazhenSyringeB: {
        name: "优质芽针针剂",
        label: "$item.YazhenSyringeB",
        expected: [
            "^优质芽针针剂$",
            "^優質芽針針劑$",
            "^Yazhen Syringe [A]$",
            "^芽針注射剤Ⅱ$",
            "^고급 야침 주사약$",
        ],
    },
    LCWulingBattery: {
        name: "低容武陵电池",
        label: "$item.LCWulingBattery",
        expected: [
            "^低容量武陵電池$",
            "^低容武陵电池$",
            "^低容量武陵電池$",
            "^저용량 무릉 배터리$",
            "^LC Wuling Battery$",
        ],
    },
    YazhenSyringe: {
        name: "芽针针剂",
        label: "$item.YazhenSyringe",
        expected: [
            "^芽針針劑$",
            "^芽针针剂$",
            "^芽針注射剤Ⅰ$",
            "^야침 주사약$",
            "^Yazhen Syringe [C]$",
        ],
    },
    JincaoDrink: {
        name: "锦草软饮",
        label: "$item.JincaoDrink",
        expected: [
            "^錦草飲料$",
            "^锦草软饮$",
            "^錦草ソーダⅠ$",
            "^금초 청량음료$",
            "^Jincao Drink$",
        ],
    },
    CuprumPart: {
        name: "赤铜零件",
        label: "$item.CuprumPart",
        expected: [
            "^赤铜零件$",
            "^赤銅零件$",
            "^Cuprium Part$",
            "^赤銅部品$",
            "^적동 부품$",
        ],
    },
    Xiranite: {
        name: "息壤",
        label: "$item.Xiranite",
        expected: [
            "^息壤$",
            "^息壤$",
            "^息壤$",
            "^식양$",
            "^Xiranite$",
        ],
    },
};

// ===== 售卖点配置 =====
// NodePrefix: pipeline_override 中使用的节点前缀
// items: 该售卖点可售卖的物品 ID 列表
const LOCATIONS = [
    {
        RegionPrefix: "ValleyIV",
        LocationId: "RefugeeCamp",
        NodePrefix: "RefugeeCamp",
        items: [
            "BuckCapsuleA",
            "HCValleyBattery",
            "CannedCitromeA",
            "SCValleyBattery",
            "CannedCitromeB",
            "BuckCapsuleB",
            "BuckCapsuleC",
            "CannedCitromeC",
            "AmethystBottle",
            "Origocrust",
            "AmethystPart",
        ],
    },
    {
        RegionPrefix: "ValleyIV",
        LocationId: "InfrastructureOutpost",
        NodePrefix: "InfrastructureOutpost",
        items: [
            "BuckCapsuleA",
            "HCValleyBattery",
            "CannedCitromeA",
            "SCValleyBattery",
            "CannedCitromeB",
            "BuckCapsuleB",
            "LCValleyBattery",
            "CannedCitromeC",
            "FerriumPart",
        ],
    },
    {
        RegionPrefix: "ValleyIV",
        LocationId: "ReconstructionCommand",
        NodePrefix: "ReconstructionCommand",
        items: [
            "BuckCapsuleA",
            "HCValleyBattery",
            "CannedCitromeA",
            "SCValleyBattery",
            "CannedCitromeB",
            "BuckCapsuleB",
            "CannedCitromeC",
            "FerriumPart",
        ],
    },
    {
        RegionPrefix: "Wuling",
        LocationId: "SkyKingFlats",
        NodePrefix: "SkyKingFlats",
        items: [
            "SCWulingBattery",
            "YazhenSyringeB",
            "LCWulingBattery",
            "YazhenSyringe",
            "JincaoDrink",
            "CuprumPart",
            "Xiranite",
        ],
    },
];

// ===== 构建 cases 数组 =====
function buildItemCases(nodePrefix, itemNum, itemIds) {
    const selectKey = `SellProduct${nodePrefix}SelectItem${itemNum}`;
    const cases = [
        {
            name: "无",
            pipeline_override: {
                [selectKey]: { enabled: false },
            },
        },
    ];
    for (const id of itemIds) {
        const item = ITEMS[id];
        cases.push({
            name: item.name,
            pipeline_override: {
                [selectKey]: {
                    enabled: true,
                    expected: item.expected,
                },
            },
            label: item.label,
        });
    }
    return cases;
}

// ===== 导出数据 =====
export default LOCATIONS.map((loc) => ({
    RegionPrefix: loc.RegionPrefix,
    LocationId: loc.LocationId,
    NodePrefix: loc.NodePrefix,
    ItemCases1: buildItemCases(loc.NodePrefix, 1, loc.items),
    ItemCases2: buildItemCases(loc.NodePrefix, 2, loc.items),
    ItemCases3: buildItemCases(loc.NodePrefix, 3, loc.items),
    ItemCases4: buildItemCases(loc.NodePrefix, 4, loc.items),
}));
