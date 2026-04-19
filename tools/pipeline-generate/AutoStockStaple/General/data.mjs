const GOODS_ROI = [
    418,
    122,
    269,
    147,
];

const GOODS_COUNT_VALIDATE_ROI = [
    1068,
    185,
    20,
    14,
];

const QUANTITY_CONTROL_TITLE_EXPECTED = [
    "购买商品",
    "購買商品",
    "(?i)Purchase",
    "購入",
];

const STOCK_THRESHOLD = 20;

const ITEMS = [
    {
        id: "ValleyEngravingPermit",
        slug: "valley_engraving_permit",
        name: "谷地刻写券",
        expected: [
            "谷地刻写券",
            "谷地刻寫券",
            "(?i)Valley\\s*Engraving\\s*Permit",
            "谷地刻印券",
            "협곡 각인권",
        ],
    },
    {
        id: "KunstTube",
        slug: "kunst_tube",
        name: "附术铁瓶",
        expected: [
            "附术铁瓶",
            "附術鐵瓶",
            "Kunst Tube",
            "付術鉄瓶",
            "아츠가 부여된 금속 병",
        ],
    },
    {
        id: "JakubsLegacy",
        slug: "jakubs_legacy",
        name: "雅各布的遗产",
        expected: [
            "雅各布的遗产",
            "雅各布的遺產",
            "(?i)Jakub's\\s*Legacy",
            "雅各布的遺產",
            "야코브의 유산",
        ],
    },
    {
        id: "PulledSlugMeat",
        slug: "pulled_slug_meat",
        name: "手撕虫肉",
        expected: [
            "手撕虫肉",
            "手撕蟲肉",
            "(?i)Pulled\\s*Slug\\s*Meat",
            "手撕蟲肉",
            "손으로 뜯은 벌레 고기",
        ],
    },
    {
        id: "HazefyreBlossom",
        slug: "hazefyre_blossom",
        name: "雾火之花",
        expected: [
            "雾火之花",
            "霧火之花",
            "(?i)Hazefyre\\s*Blossom",
            "霧火之花",
            "화염 불꽃",
        ],
    },
    {
        id: "CartilageCookie",
        slug: "cartilage_cookie",
        name: "软骨饼干",
        expected: [
            "软骨饼干",
            "軟骨餅乾",
            "(?i)Cartilage\\s*Cookie",
            "軟骨餅乾",
            "뼈 쿠키",
        ],
    },
    {
        id: "CosmoMeltoJelly",
        slug: "cosmo_melto_jelly",
        name: "星融果冻",
        expected: [
            "星融果冻",
            "星融果凍",
            "(?i)Cosmo\\s*Melto\\s*Jelly",
            "星融果凍",
            "코스모 멜토 젤리",
        ],
    },
    {
        id: "KeenValleyDetector",
        slug: "keen_valley_detector",
        name: "新锐谷地探物器",
        expected: [
            "新锐谷地探物器",
            "新銳谷地探物器",
            "(?i)Keen\\s*Valley\\s*Detector",
            "新銳谷地探物器",
            "신예 협곡 탐지기",
        ],
    },
    {
        id: "KeenValleyCompass",
        slug: "keen_valley_compass",
        name: "新锐谷地罗盘",
        expected: [
            "新锐谷地罗盘",
            "新銳谷地羅盤",
            "(?i)Keen\\s*Valley\\s*Compass",
            "新銳谷地羅盤",
            "신예 협곡 나이프",
        ],
    },
    {
        id: "BreezeOfKjersch",
        slug: "breeze_of_kjersch",
        name: "耶尔什微风",
        expected: [
            "耶尔什微风",
            "耶爾什微風",
            "(?i)Breeze\\s*Of\\s*Kjersch",
            "耶爾什微風",
            "예르시 미풍",
        ],
    },
    {
        id: "TidestoneSpecimen",
        slug: "tidestone_specimen",
        name: "浪潮海石",
        expected: [
            "浪潮海石",
            "浪潮海石",
            "(?i)Tidestone\\s*Specimen",
            "浪潮海石",
            "파도 바위",
        ],
    },
    {
        id: "SimonchShawl",
        slug: "simonch_shawl",
        name: "塞梦珂披巾",
        expected: [
            "塞梦珂披巾",
            "塞夢珂披巾",
            "(?i)Simonch\\s*Shawl",
            "塞夢珂披巾",
            "시몬치 셔워",
        ],
    },
    {
        id: "FrontiersWatch",
        slug: "frontiers_watch",
        name: "《前沿瞭望》",
        expected: [
            "《前沿瞭望》",
            "《前沿瞭望》",
            "(?i)Frontiers\\s*Watch",
            "《前沿瞭望》",
            "《프론티어스 와치》",
        ],
    },
    {
        id: "AuryleneCamera",
        slug: "aurylene_camera",
        name: "醚质相机",
        expected: [
            "醚质相机",
            "醚質相機",
            "(?i)Aurylene\\s*Camera",
            "醚質相機",
            "아우리렌 카메라",
        ],
    },
    {
        id: "CosmicGatePuzzle",
        slug: "cosmic_gate_puzzle",
        name: "星门锁",
        expected: [
            "星门锁",
            "星門鎖",
            "(?i)Cosmic\\s*Gate\\s*Puzzle",
            "星門鎖",
            "코스믹 게이트 퍼즐",
        ],
    },
    {
        id: "SurfingFestivalTicket",
        slug: "surfing_festival_ticket",
        name: "冲浪大赛门票",
        expected: [
            "冲浪大赛门票",
            "衝浪大賽門票",
            "(?i)Surfing\\s*Festival\\s*Ticket",
            "衝浪大賽門票",
            "서핑대회 입장권",
        ],
    },
    {
        id: "WulingEngravingPermit",
        slug: "wuling_engraving_permit",
        name: "武陵刻写券",
        expected: [
            "武陵刻写券",
            "武陵刻寫券",
            "(?i)Wuling\\s*Engraving\\s*Permit",
            "武陵刻印券",
            "월릉 각인권",
        ],
    },
    {
        id: "FortifyingInfusion",
        slug: "fortifying_infusion",
        name: "正本补元汤剂",
        expected: [
            "正本补元汤剂",
            "正本補元湯劑",
            "(?i)Fortifying\\s*Infusion",
            "正本補元湯劑",
            "정본 보충 육탕제",
        ],
    },
    {
        id: "GardenFriedRice",
        slug: "garden_fried_rice",
        name: "锦素炒饭",
        expected: [
            "锦素炒饭",
            "錦素炒飯",
            "(?i)Garden\\s*Fried\\s*Rice",
            "錦素炒飯",
            "원예 죽밥",
        ],
    },
    {
        id: "MasterPansEggPudding",
        slug: "master_pans_egg_pudding",
        name: "潘师傅蛋羹",
        expected: [
            "潘师傅蛋羹",
            "潘師傅蛋羹",
            "(?i)Master\\s*Pan's\\s*Egg\\s*Pudding",
            "潘師傅蛋羹",
            "판 스토커 달걀 푸딩",
        ],
    },
    {
        id: "EchoingRemedy",
        slug: "echoing_remedy",
        name: "回响秘剂",
        expected: [
            "回响秘剂",
            "迴響秘劑",
            "(?i)Echoing\\s*Remedy",
            "回響の秘薬",
            "메아리의 비약",
        ],
    },
    {
        id: "WulingArtificingCatalyst",
        slug: "wuling_artificing_catalyst",
        name: "武陵精锻助剂",
        expected: [
            "武陵精锻助剂",
            "武陵精鍛助劑",
            "(?i)Wuling\\s*Artificing\\s*Catalyst",
            "武陵精鍛助劑",
            "월릉 정련 도움제",
        ],
    },
    {
        id: "KeenWulingDetector",
        slug: "keen_wuling_detector",
        name: "新锐武陵探物器",
        expected: [
            "新锐武陵探物器",
            "新銳武陵探物器",
            "(?i)Keen\\s*Wuling\\s*Detector",
            "新銳武陵探物器",
            "신예 월릉 탐지기",
        ],
    },
    {
        id: "KeenWulingCompass",
        slug: "keen_wuling_compass",
        name: "新锐武陵罗盘",
        expected: [
            "新锐武陵罗盘",
            "新銳武陵羅盤",
            "(?i)Keen\\s*Wuling\\s*Compass",
            "新銳武陵羅盤",
            "신예 월릉 나이프",
        ],
    },
    {
        id: "EurekaTeabox",
        slug: "eureka_teabox",
        name: "岳研茶盒",
        expected: [
            "岳研茶盒",
            "岳研茶盒",
            "(?i)Eureka\\s*Teabox",
            "岳研茶盒",
            "예루카 티박스",
        ],
    },
    {
        id: "YinglungDumbbells",
        slug: "yinglung_dumbbells",
        name: "应龙哑铃",
        expected: [
            "应龙哑铃",
            "應龍啞鈴",
            "(?i)Yinglung\\s*Dumbbells",
            "應龍啞鈴",
            "영룡 덤벨",
        ],
    },
    {
        id: "WovenAggelos",
        slug: "woven_aggelos",
        name: "草编天使",
        expected: [
            "草编天使",
            "草編天使",
            "(?i)Woven\\s*Aggelos",
            "草編天使",
            "초반 천사",
        ],
    },
    {
        id: "ScrimshawOfThePack",
        slug: "scrimshaw_of_the_pack",
        name: "狼群骨雕",
        expected: [
            "狼群骨雕",
            "狼群骨雕",
            "(?i)Scrimshaw\\s*Of\\s*The\\s*Pack",
            "狼群骨雕",
            "늑대 뼈 문신",
        ],
    },
    {
        id: "ChubbyLungFangxingMemorabilia",
        slug: "chubby_lung_fangxing_memorabilia",
        name: "龙泡泡·方兴纪念",
        expected: [
            "龙泡泡·方兴纪念",
            "龍泡泡·方興紀念",
            "(?i)Chubby\\s*Lung:\\s*Fangxing\\s*Memorabilia",
            "龍泡泡・方興記念",
            "드래곤 버블 · 방흥 에디션",
        ],
    },
];

function buildGoods(items) {
    const result = {};

    for (const item of items) {
        result[`AutoStockStapleGoods${item.id}`] = {
            desc: item.name,
            recognition: {
                type: "OCR",
                param: {
                    roi: GOODS_ROI,
                    expected: item.expected,
                },
            },
        };
    }

    return result;
}

function buildGoodsCountValidate(items) {
    const result = {
        AutoStockStapleGoodsCountValidate: {
            desc: "物品数量验证",
            recognition: {
                type: "OCR",
                param: {
                    roi: GOODS_COUNT_VALIDATE_ROI,
                    expected: [
                        "\\d+",
                    ],
                    only_rec: true,
                },
            },
        },
    };

    for (const item of items) {
        result[`AutoStockStapleGoods${item.id}Validate`] = {
            desc: `${item.name}数量验证`,
            recognition: {
                type: "Custom",
                param: {
                    custom_recognition: "ExpressionRecognition",
                    custom_recognition_param: {
                        expression: `${STOCK_THRESHOLD} > {AutoStockStapleGoodsCountValidate}`,
                    },
                },
            },
        };

        result[`AutoStockStapleGoods${item.id}ExcludeValidate`] = {
            desc: `${item.name}数量排除验证`,
            recognition: {
                type: "Custom",
                param: {
                    custom_recognition: "ExpressionRecognition",
                    custom_recognition_param: {
                        expression: `${STOCK_THRESHOLD} <= {AutoStockStapleGoodsCountValidate}`,
                    },
                },
            },
        };
    }

    return result;
}

function buildQuantityControl(items) {
    const result = {
        AutoStockStapleQuantityControl: {
            desc: "购买商品界面的数量控制",
            recognition: {
                type: "OCR",
                param: {
                    roi: [
                        55,
                        53,
                        333,
                        167,
                    ],
                    expected: QUANTITY_CONTROL_TITLE_EXPECTED,
                },
            },
            pre_delay: 0,
            post_delay: 0,
            rate_limit: 0,
            next: items.map((item) => `AutoStockStapleQuantityControl${item.id}`),
        },
    };

    for (const item of items) {
        const controlNode = `AutoStockStapleQuantityControl${item.id}`;
        const goodsNode = `AutoStockStapleGoods${item.id}`;
        const validateNode = `${goodsNode}Validate`;
        const excludeValidateNode = `${goodsNode}ExcludeValidate`;

        result[controlNode] = {
            desc: `${item.name}的数量控制`,
            recognition: {
                type: "And",
                param: {
                    all_of: [
                        goodsNode,
                    ],
                },
            },
            pre_delay: 0,
            post_delay: 0,
            rate_limit: 0,
            next: [
                `${controlNode}Buy`,
                `${controlNode}Exclude`,
            ],
        };

        result[`${controlNode}Buy`] = {
            desc: `${item.name}低于阈值时购买指定数量`,
            recognition: {
                type: "And",
                param: {
                    all_of: [
                        goodsNode,
                        validateNode,
                    ],
                },
            },
            pre_delay: 0,
            action: {
                type: "Custom",
                param: {
                    custom_action: "AutoStockStapleQuantityControlAction",
                    custom_action_param: {
                        item_name: item.slug,
                    },
                },
            },
            post_delay: 0,
            rate_limit: 0,
            next: [
                "AutoStockStapleQuantityControlConfirmBuy",
            ],
        };

        result[`${controlNode}Exclude`] = {
            desc: `${item.name}达到阈值后排除`,
            recognition: {
                type: "And",
                param: {
                    all_of: [
                        goodsNode,
                        excludeValidateNode,
                    ],
                },
            },
            pre_delay: 0,
            action: {
                type: "Custom",
                param: {
                    custom_action: "PipelineOverrideAction",
                    custom_action_param: {
                        patch: {
                            AutoStockInStapleItemName: {
                                attach: {
                                    [item.slug]: false,
                                },
                            },
                        },
                    },
                },
            },
            post_delay: 0,
            rate_limit: 0,
            next: [
                "AutoStockStapleQuantityControlResetRecognitionParams",
            ],
        };
    }

    result.AutoStockStapleQuantityControlConfirmBuy = {
        desc: "确认购买",
        recognition: {
            type: "And",
            param: {
                all_of: [
                    "AutoStockStapleQuantityControl",
                    "YellowConfirmButtonType1",
                ],
                box_index: 1,
            },
        },
        action: {
            type: "Click",
        },
        next: [
            "AutoStockStapleQuantityControlCloseRewards",
        ],
    };

    result.AutoStockStapleQuantityControlResetRecognitionParams = {
        desc: "重置识别参数",
        recognition: {
            type: "And",
            param: {
                all_of: [
                    "AutoStockStapleQuantityControl",
                ],
            },
        },
        action: {
            type: "Custom",
            param: {
                custom_action: "AttachToExpectedRegexAction",
                custom_action_param: {
                    target: "AutoStockInStapleItemName",
                },
            },
        },
        next: [
            "AutoStockStapleQuantityControlCloseBuyWindow",
        ],
    };

    result.AutoStockStapleQuantityControlCloseBuyWindow = {
        desc: "关闭购买窗口",
        recognition: {
            type: "And",
            param: {
                all_of: [
                    "CloseButtonType1",
                ],
            },
        },
        action: {
            type: "Click",
        },
    };

    result.AutoStockStapleQuantityControlCloseRewards = {
        desc: "关闭奖励窗口",
        recognition: {
            type: "And",
            param: {
                all_of: [
                    "CloseRewardsButton",
                ],
            },
        },
        action: {
            type: "Click",
        },
    };

    return result;
}

export default [
    {
        Goods: buildGoods(ITEMS),
        GoodsCountValidate: buildGoodsCountValidate(ITEMS),
        QuantityControl: buildQuantityControl(ITEMS),
    },
];
