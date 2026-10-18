// Skland 地图自动标记助手
//
// 运行上下文：MaaEnd 的 WebView2 悬浮窗（skland_map_enable），由
// WebView2::AddScriptToExecuteOnDocumentCreated 在 document-start 注入。
//
// 数据来源分两路：
//   1. 游戏画面侧：MaaEnd 的 Pipeline 识别到事件后，通过 ExecuteScript 调用
//      window.__maaMapMark.onEvent({ type, at }) 推过来；
//   2. 地图页面侧：本脚本 hook WebSocket 拿实时位置（type 1012），
//      并通过 React fiber 找到活着的 mapInfo 实例调用页面自身的 API。
//
// 坐标系：WS 的 pos {x,y,z} 等于 mark/list 的 pos {x,y,z}；
// store 内部的 center {lng,lat,alt} = {x,z,y}。匹配一律用三维距离。
(function () {
    'use strict';
    if (window.__maaMapMark) {
        return;
    }

    // ---------- 可替换配置（模板/阈值都集中在这里） ----------

    // 事件类型 -> 点位模板。templateId 是服务端下发、跨语言稳定的主判据；
    // namePatterns 只作为templateId 变化时的兜底（中/繁/英/日）。
    // 数据来自 /map/mark/list 的 markTemplates，覆盖国服当前版本。
    const TYPE_RULES = {
        chest: {
            templateIds: [
                '69cbe7f3b65f307b3b4c31daa4294e75', // 储藏箱I
                '3f2a41a932d0645beadc319bad088231', // 储藏箱II
                '2072475f2129875503a84d3c632c5197', // 储藏箱III
                '46d6a88c6fb6db31c6c6f73f9d43b6f5', // 储藏箱IV
                '6de64a17d73518f5ee7800de95d3c523', // 锁闭储藏箱
                'fc5fdd8cc5b2b77f9a1fdf1c94d5f738', // 装备模板箱
            ],
            namePatterns: [/储藏箱/, /鎖閉儲藏箱/, /锁闭储藏箱/, /装备模板箱/, /裝備模板箱/, /chest/i, /storage\s*box/i],
            dyMax: 5.0,
            horizMax: 30,
            ambiguityGap: 10,
        },
        essence: {
            templateIds: [
                '9c7252a186b833b68b74ab8ddd8a6e61', // 醚质
                '5537623b30191215f654afe44db3f6fe', // 醚质
            ],
            namePatterns: [/醚质/, /醚質/, /ether/i, /essence/i],
            dyMax: 8.0,
            horizMax: 60,
            ambiguityGap: 20,
        },
        crystal: {
            templateIds: [
                '5f1940375f346382e8880000e982dacd', // 塔晶
                '36058795c0425157ec814002e6c6b5d7', // 塔晶
            ],
            namePatterns: [/塔晶/, /crystal/i, /tower/i],
            dyMax: 8.0,
            horizMax: 60,
            ambiguityGap: 20,
        },
        archive: {
            templateIds: [
                '6445e8149d6257b79dbadf22f1167492',
                '6a0d3aab98dfdf3a2543dd32cabfe163',
                '8b171b6b65ad749185233e072fee1abf',
                '1290aebe2c902eed7b72c87c4b5308f7',
                'c60011bfe65a4e7752c2d8a6e6eb581c',
                'ca65717addcd70bd7196e7ecfec29680',
                'a2a597ec1e27e59a1688d517f3c4c9eb',
                'e621ca3750b53a807b0f21f0c4091e73',
                '5a935c4088380dab806208464b631ff7',
                'f5179ac1c49f834a8fa5ab12f3edda36',
                '221ff6c7f565526b8e75643f114a5ef0',
                '85fecfef85e0f3d74602b1a2f816f225',
                '5079ff5183c248ee3106c2f1bdf8b56e',
                'b21b6610589ef2fc64256c866708c7c9', // 音像存档
                'b128d322772da2ad726f3a047832f9d8', // 音像存档
            ],
            namePatterns: [/见闻辑录/, /見聞輯錄/, /音像存档/, /音像存檔/, /archive/i, /record/i],
            dyMax: 5.0,
            horizMax: 20,
            ambiguityGap: 10,
        },
    };

    // 开关“打开”后多久内出现“获得”才认定是一次开箱
    const OPEN_TO_GAIN_MS = 3000;
    // 同一类事件的去抖窗口，Pipeline 每轮都会重复命中
    const DEBOUNCE_MS = 600;

    // ---------- 运行时状态 ----------

    const state = {
        loc: null,          // 最新位置帧
        lastTypeAt: {},     // type -> 上次事件时刻，用于去抖
        openAt: 0,          // 最近一次“打开”提示的时刻
        queue: [],          // 待确认项
        log: [],            // 事件日志，便于离线复盘
        autoMarked: 0,
        busy: false,        // 标记请求进行中，期间丢弃新事件避免并发写
    };

    // ---------- 位置：hook 页面自己的 WebSocket ----------

    const NativeWS = window.WebSocket;
    if (NativeWS) {
        window.WebSocket = new Proxy(NativeWS, {
            construct(target, args) {
                const ws = new target(...args);
                try {
                    ws.addEventListener('message', (ev) => {
                        if (typeof ev.data !== 'string') {
                            return;
                        }
                        let msg;
                        try {
                            msg = JSON.parse(ev.data);
                        } catch (e) {
                            return;
                        }
                        if (msg && msg.type === 1012 && msg.data && msg.data.pos) {
                            state.loc = {
                                pos: msg.data.pos,
                                mapId: msg.data.mapId,
                                levelId: msg.data.levelId,
                                isOnline: msg.data.isOnline,
                                at: Date.now(),
                            };
                        }
                    });
                } catch (e) {
                    /* 事件绑定失败不影响页面本身 */
                }
                return ws;
            },
        });
    }

    // ---------- 地图实例：从 React fiber 树上找活着的 mapInfo ----------

    function findMapInfo() {
        const el = document.getElementById('root') || document.body;
        if (!el) {
            return null;
        }
        const key = Object.keys(el).find((k) => k.startsWith('__reactContainer$'));
        if (!key) {
            return null;
        }
        let fiber = el[key];
        if (fiber && fiber.current) {
            fiber = fiber.current;
        }
        const seen = new Set();
        const stack = [fiber];
        while (stack.length) {
            const node = stack.pop();
            if (!node || typeof node !== 'object' || seen.has(node)) {
                continue;
            }
            seen.add(node);
            const sn = node.stateNode;
            if (sn && typeof sn === 'object') {
                try {
                    if (sn.mapInfo) {
                        return sn.mapInfo;
                    }
                } catch (e) {
                    /* getter 抛错则跳过 */
                }
            }
            if (node.child) {
                stack.push(node.child);
            }
            if (node.sibling) {
                stack.push(node.sibling);
            }
        }
        return null;
    }

    // ---------- 匹配 ----------

    function distance(pos, center) {
        const dx = center.lng - pos.x;
        const dy = center.alt - pos.y;
        const dz = center.lat - pos.z;
        return Math.sqrt(dx * dx + dy * dy + dz * dz);
    }

    function horizDistance(pos, center) {
        const dx = center.lng - pos.x;
        const dz = center.lat - pos.z;
        return Math.sqrt(dx * dx + dz * dz);
    }

    function templateName(infos, templateId) {
        const t = infos.templateById[templateId];
        return t ? t.name : '';
    }

    function collectCandidates(type) {
        const rule = TYPE_RULES[type];
        const mi = findMapInfo();
        if (!mi) {
            return { error: '地图实例未就绪' };
        }
        const model = mi.gameMapOModel;
        if (!model || !model.markerPointMap) {
            return { error: 'markerPointMap 不存在' };
        }
        if (!state.loc) {
            return { error: '没有位置数据，确认地图右下角【位置同步】已打开' };
        }
        if (state.loc.isOnline === false) {
            return { error: '游戏不在线，位置帧已失效' };
        }
        const infos = buildTemplateIndex(mi);
        const level = state.loc.levelId;
        const out = [];
        const pm = model.markerPointMap;
        for (const id in pm) {
            const p = pm[id];
            if (p.isMarked) {
                continue;
            }
            if (level && p.levelId && p.levelId !== level) {
                continue;
            }
            const name = templateName(infos, p.templateId);
            const byId = rule.templateIds.indexOf(p.templateId) !== -1;
            if (!byId && !rule.namePatterns.some((re) => re.test(name))) {
                continue;
            }
            const d3 = distance(state.loc.pos, p.center);
            const dh = horizDistance(state.loc.pos, p.center);
            const dy = Math.abs(p.center.alt - state.loc.pos.y);
            if (dy > rule.dyMax || dh > rule.horizMax) {
                continue;
            }
            out.push({ id, name, d3, dh, dy, levelId: p.levelId });
        }
        out.sort((a, b) => a.d3 - b.d3);
        return { candidates: out, rule };
    }

    function buildTemplateIndex(mi) {
        const model = mi.gameMapOModel;
        const templateById = {};
        const list = (model && model.markerTemplateMap) || {};
        for (const id in list) {
            templateById[id] = list[id];
        }
        if (!Object.keys(templateById).length) {
            // 某些版本把模板存在 markerTypeList 的子项里，兜底扫一遍
            const types = (model && model.markerTypeList) || [];
            types.forEach((t) => {
                (t.subTypes || []).forEach((s) => {
                    if (s && s.id) {
                        templateById[s.id] = s;
                    }
                });
            });
        }
        return { templateById };
    }

    // ---------- 标记 ----------

    async function markPoint(id) {
        const mi = findMapInfo();
        if (!mi) {
            throw new Error('地图实例未就绪');
        }
        const model = mi.gameMapOModel;
        const p = model.markerPointMap[id];
        if (!p) {
            throw new Error('点位不存在: ' + id);
        }
        if (p.isMarked) {
            return { skipped: 'already-marked' };
        }
        const r = await model.fetchMarkMarkerPoint(id, mi.mapId);
        if (r && r.code === 0) {
            state.autoMarked += 1;
        }
        return r;
    }

    // ---------- 事件处理 ----------

    function now() {
        return Date.now();
    }

    async function handleEvent(type) {
        const t = now();
        const last = state.lastTypeAt[type] || 0;
        if (t - last < DEBOUNCE_MS) {
            return;
        }
        state.lastTypeAt[type] = t;
        // “打开”只记录时刻，等随后的“获得”。这一步不能受 busy 影响：
        // 丢了这一刻，紧随其后的“获得”就会被当成孤立事件，开箱不生效。
        if (type === 'open') {
            state.openAt = t;
            return;
        }

        // “获得”必须挂在一个最近出现过的“打开”后面，才算一次开箱
        let resolved = type;
        if (type === 'gain') {
            if (t - state.openAt > OPEN_TO_GAIN_MS) {
                state.log.push({ at: t, type: 'gain-orphan' });
                return;
            }
            resolved = 'chest';
            state.openAt = 0;
        }

        // 只有真正要写点位时才需要独占：上一次标记还没回来就跳过这一次，
        // 时刻与关联已经在上面处理完，不会丢。
        if (state.busy) {
            state.log.push({ at: t, type: resolved, result: 'busy-skip' });
            return;
        }

        const got = collectCandidates(resolved);
        if (got.error) {
            state.log.push({ at: t, type: resolved, error: got.error });
            return;
        }
        const list = got.candidates || [];
        if (!list.length) {
            state.log.push({ at: t, type: resolved, result: 'no-candidate' });
            return;
        }

        const top = list[0];
        const second = list[1];
        const ambiguous = second && second.d3 - top.d3 < got.rule.ambiguityGap;

        if (ambiguous) {
            // 歧义：不自动标，进待确认（同类型同候选不重复入队）
            if (state.queue.some((q) => q.type === resolved && q.top.id === top.id)) {
                return;
            }
            state.queue.push({ at: t, type: resolved, top, second, loc: state.loc, reason: 'ambiguous' });
            refreshTitle();
            state.log.push({ at: t, type: resolved, result: 'queued', top: top.id, topDist: top.d3, secondDist: second.d3 });
            return;
        }

        state.busy = true;
        try {
            const r = await markPoint(top.id);
            state.log.push({ at: t, type: resolved, result: 'marked', id: top.id, name: top.name, dist: top.d3, code: r && r.code });
        } catch (e) {
            state.log.push({ at: t, type: resolved, result: 'error', error: String(e) });
        } finally {
            state.busy = false;
        }
    }

    // ---------- 待确认 ----------

    function refreshTitle() {
        if (!document.title) {
            return;
        }
        const base = document.title.replace(/\s*\(\d+\)$/, '');
        document.title = state.queue.length ? base + ' (' + state.queue.length + ')' : base;
    }

    async function confirm(index) {
        const item = state.queue[index];
        if (!item) {
            return false;
        }
        const r = await markPoint(item.top.id);
        state.queue.splice(index, 1);
        refreshTitle();
        return r;
    }

    function reject(index) {
        if (!state.queue[index]) {
            return false;
        }
        state.log.push({ at: now(), type: 'manual-reject', id: state.queue[index].top.id });
        state.queue.splice(index, 1);
        refreshTitle();
        return true;
    }

    // ---------- 对外接口 ----------

    window.__maaMapMark = {
        version: '0.1.0',

        // MaaEnd 在开始识别循环前会轮询这个标记。
        // 地图实例已创建且登录态就绪，才认为悬浮窗可以被自动标记使用。
        ready() {
            return !!findMapInfo() && !!window.localStorage.getItem('SK_OAUTH_CRED_KEY');
        },

        // MaaEnd 通过 ExecuteScript 调用：onEvent({ type, at })
        onEvent(payload) {
            const type = payload && payload.type;
            if (!type) {
                return false;
            }
            handleEvent(type).catch((e) => {
                state.log.push({ at: now(), type, error: String(e) });
            });
            return true;
        },

        // 调试：查看附近候选
        nearby(type, limit) {
            const got = collectCandidates(type || 'chest');
            if (got.error) {
                return got;
            }
            return { loc: state.loc, candidates: got.candidates.slice(0, limit || 5) };
        },

        // 调试 / 手动
        status() {
            return {
                loc: state.loc,
                queue: state.queue.length,
                autoMarked: state.autoMarked,
                log: state.log.slice(-20),
            };
        },
        queue: () => state.queue.slice(),
        confirm,
        reject,
    };
})();
