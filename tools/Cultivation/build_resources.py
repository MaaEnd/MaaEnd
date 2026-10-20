"""Generate only native prototype resources; no device or SRC imports."""
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def write(path, data):
    target = ROOT / path
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(json.dumps(data, ensure_ascii=False, indent=4) + '\n', encoding='utf-8')


def action(step, **kwargs):
    return {'type': 'Custom', 'param': {'custom_action': 'CultivationAction',
            'custom_action_param': {'step': step, **kwargs}}}


def guard(tab=None):
    return {'type': 'Custom', 'param': {'custom_recognition': 'CultivationGuard',
            'custom_recognition_param': {} if tab is None else {'tab': tab}, 'roi': [420, 75, 810, 160]}}


def node(operation, next_nodes=None, recognition='DirectHit', **extra):
    return {'recognition': recognition, 'action': operation, 'pre_delay': 0,
            'post_delay': 0, 'rate_limit': 0, 'next': next_nodes or [], **extra}


def build():
    pipeline = {'CultivationScan': node(action('begin'), ['CultivationP0T0Select'])}
    for p in range(2):
        for tab, x in enumerate([568, 750, 952, 1143]):
            prefix = f'CultivationP{p}T{tab}'
            after = (f'CultivationP{p}T{tab+1}Select' if tab < 3 else
                     'CultivationP1T0Select' if p == 0 else 'CultivationFinish')
            phase = {'tab': tab, 'pass': p}
            freeze = {'time': 300, 'target': [425, 250, 813, 415], 'threshold': 0.98}
            pipeline[prefix+'Select'] = node({'type': 'Click', 'param': {'target': [x, 207]}},
                                             [prefix+'Reset'], guard(), post_wait_freezes=freeze)
            pipeline[prefix+'Reset'] = node(action('reset', **phase), [prefix+'Top'], guard(tab))
            pipeline[prefix+'Top'] = node(action('top', **phase, again=prefix+'TopSwipe', done=prefix+'Read'),
                                          [prefix+'TopSwipe', prefix+'Read'], guard(tab))
            pipeline[prefix+'TopSwipe'] = node({'type': 'Swipe', 'param': {
                'begin': [640, 380], 'end': [640, 620], 'duration': 900}},
                [prefix+'Top'], guard(tab), post_wait_freezes=freeze)
            pipeline[prefix+'Read'] = node(action('read', **phase), [prefix+'DownSwipe'], guard(tab))
            pipeline[prefix+'DownSwipe'] = node({'type': 'Swipe', 'param': {
                'begin': [640, 620], 'end': [640, 380], 'duration': 900}},
                [prefix+'Bottom'], guard(tab), post_wait_freezes=freeze)
            pipeline[prefix+'Bottom'] = node(action('bottom', **phase, again=prefix+'Read', done=after),
                                             [prefix+'Read', after], guard(tab))
    pipeline['CultivationFinish'] = node(action('finish'), recognition=guard())
    for name, step in [('Preview', 'preview'), ('Export', 'export'), ('Clear', 'clear')]:
        pipeline['Cultivation'+name] = node(action(step))
    pipeline['CultivationOCR'] = node('DoNothing', recognition={
        'type': 'OCR', 'param': {'roi': [420, 75, 810, 590]}})
    pipeline['CultivationIcon'] = node('DoNothing', recognition={
        'type': 'Custom', 'param': {'custom_recognition': 'IconRecognition',
        'custom_recognition_param': {'grid_type': 'single_roi',
                                   'item_filters': ['ValuableDepot:*', 'Normal:*', 'Isolate:Gold']},
        'roi': [910, 386, 80, 80]}})
    write('assets/resource/pipeline/Cultivation/Cultivation.json', pipeline)
    tasks = []
    for name in ['Scan', 'Preview', 'Export', 'Clear']:
        tasks.append({'name': 'Cultivation'+name, 'label': '$task.Cultivation'+name+'.label',
                      'entry': 'Cultivation'+name, 'group': ['other_menu'], 'controller': ['CloudADB']})
    write('assets/tasks/Cultivation.json', {'task': tasks})
    path = ROOT/'assets/interface.json'
    text = path.read_text(encoding='utf-8')
    if '"tasks/Cultivation.json"' not in text:
        text = text.replace('"tasks/PullCountCalculator.json",', '"tasks/PullCountCalculator.json",\n        "tasks/Cultivation.json",')
        path.write_text(text, encoding='utf-8')
    translations = {
        'zh_cn': ['读取培养需求（原生试作）', '预览培养需求', '生成刷取目标预览', '清空培养需求',
                  '培养读取失败；保留原计划，请核对当前页面和日志。', '已清空培养需求。',
                  '培养计划：%s / %s；人工材料 %d 类。已有 / 需要（缺口）：',
                  '已生成 CultivationSupplyPlan.preview.json。此试作只预览目标，不启动刷取。'],
        'zh_tw': ['讀取培養需求（原生試作）', '預覽培養需求', '產生刷取目標預覽', '清空培養需求',
                  '培養讀取失敗；保留原計畫，請核對目前頁面和日誌。', '已清空培養需求。',
                  '培養計畫：%s / %s；人工材料 %d 類。擁有 / 需要（缺口）：',
                  '已產生 CultivationSupplyPlan.preview.json。此試作僅預覽目標，不啟動刷取。'],
        'en_us': ['Read cultivation needs (native prototype)', 'Preview cultivation needs', 'Export farming goal preview', 'Clear cultivation needs',
                  'Cultivation read failed; previous plan retained. Check the page and logs.', 'Cultivation plan cleared.',
                  'Cultivation plan: %s / %s; %d manual material types. Owned / required (gap):',
                  'CultivationSupplyPlan.preview.json exported. This prototype previews goals without starting farming.'],
        'ja_jp': ['育成素材を読み取る（試作）', '育成素材を確認', '周回目標のプレビューを出力', '育成計画を消去',
                  '読み取り失敗。以前の計画を保持しました。画面とログを確認してください。', '育成計画を消去しました。',
                  '育成計画：%s / %s。手動素材 %d 種。所持 / 必要（不足）：',
                  'CultivationSupplyPlan.preview.json を出力しました。この試作版は周回を開始しません。'],
        'ko_kr': ['육성 재료 읽기 (시험판)', '육성 재료 미리 보기', '파밍 목표 미리 보기 내보내기', '육성 계획 지우기',
                  '읽기에 실패했습니다. 이전 계획을 유지합니다. 화면과 로그를 확인하세요.', '육성 계획을 지웠습니다.',
                  '육성 계획: %s / %s. 수동 재료 %d종. 보유 / 필요 (부족):',
                  'CultivationSupplyPlan.preview.json을 내보냈습니다. 이 시험판은 파밍을 시작하지 않습니다.'],
    }
    for lang, values in translations.items():
        path = ROOT/f'assets/locales/interface/{lang}.json'
        data = json.loads(path.read_text(encoding='utf-8'))
        for suffix, label in zip(['Scan', 'Preview', 'Export', 'Clear'], values[:4]):
            data[f'task.Cultivation{suffix}.label'] = label
        for key, value in zip(['failed', 'cleared', 'preview', 'exported'], values[4:]):
            data['cultivation.'+key] = value
        data['cultivation.weapon'] = {
            'zh_cn': '武器：%s。基质状态未验收，单独保留为人工待办，不进入材料刷取目标。',
            'zh_tw': '武器：%s。基質狀態未驗收，另列人工待辦，不進入材料刷取目標。',
            'en_us': 'Weapon: %s. Essence readiness is unverified; tracked separately for manual review, outside material farming goals.',
            'ja_jp': '武器：%s。基質の状態は未確認です。素材周回の対象には含めず、手動確認として保持します。',
            'ko_kr': '무기: %s. 기질 상태는 미확인입니다. 재료 파밍 목표와 분리하여 수동 확인 항목으로 유지합니다.'
        }[lang]
        write(str(path.relative_to(ROOT)), data)
    for kind, name, ref in [('action', 'CultivationAction', 'Action'), ('recognition', 'CultivationGuard', 'Guard')]:
        path = ROOT/f'tools/schema/custom.{kind}.schema.json'
        # Preserve unrelated upstream formatting in these shared schema files.
        original = path.read_text(encoding='utf-8')
        parsed = json.loads(original)
        if name in parsed['properties']['custom_'+kind]['enum']:
            assert any(item.get('if', {}).get('properties', {}).get('custom_'+kind, {}).get('const') == name
                       for item in parsed.get('allOf', [])), 'Registration exists without matching parameter schema'
            continue
        condition = {'if': {'properties': {'custom_'+kind: {'const': name}}, 'required': ['custom_'+kind]},
            'then': {'properties': {'custom_'+kind+'_param': {'$ref': './components/cultivation.schema.json#/$defs/'+ref}},
                     'required': ['custom_'+kind+'_param']}}
        original = re.sub(r'("custom_'+kind+r'":\s*\{\s*"enum":\s*\[)',
                          lambda match: match[0]+'\n                '+json.dumps(name)+',', original, count=1)
        addition = '\n'.join('        '+line for line in json.dumps(condition, indent=4).splitlines())
        original = re.sub(r'("allOf":\s*\[)', lambda match: match[0]+'\n'+addition+',', original, count=1)
        path.write_text(original, encoding='utf-8')
    write('tools/schema/components/cultivation.schema.json', {'$schema': 'https://json-schema.org/draft/2020-12/schema', '$defs': {
        'Action': {'type': 'object', 'required': ['step'], 'additionalProperties': False, 'properties': {
            'step': {'enum': ['begin', 'reset', 'top', 'read', 'bottom', 'finish', 'preview', 'export', 'clear']},
            'tab': {'type': 'integer', 'minimum': 0, 'maximum': 3},
            'pass': {'type': 'integer', 'minimum': 0, 'maximum': 1},
            'again': {'type': 'string', 'pattern': '^Cultivation'}, 'done': {'type': 'string', 'pattern': '^Cultivation'}}},
        'Guard': {'type': 'object', 'additionalProperties': False,
                  'properties': {'tab': {'type': 'integer', 'minimum': 0, 'maximum': 3}}}}})
    print(f'Generated {len(pipeline)} native nodes, four tasks, five locale entries, and Custom schemas.')


if __name__ == '__main__':
    build()
