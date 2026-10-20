"""Generate only native prototype resources; no device or SRC imports."""
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def write(path, data):
    target = ROOT / path
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(json.dumps(data, ensure_ascii=False, indent=4) + '\n', encoding='utf-8', newline='\n')


def action(step, **kwargs):
    return {'type': 'Custom', 'param': {'custom_action': 'CultivationAction',
            'custom_action_param': {'step': step, **kwargs}}}


def guard(tab=None):
    return {'type': 'Custom', 'param': {'custom_recognition': 'CultivationGuard',
            'custom_recognition_param': {} if tab is None else {'tab': tab}, 'roi': [420, 75, 810, 160]}}


def node(operation, next_nodes=None, recognition='DirectHit', **extra):
    return {'recognition': recognition, 'action': operation, 'pre_delay': 0,
            'post_delay': 0, 'rate_limit': 0, 'next': next_nodes or [], **extra}


# Left operator list and the selected operator's name in the overview header.
OPERATOR_LIST = [30, 100, 350, 520]
OPERATOR_NAME = [480, 85, 300, 40]
OPERATOR_FREEZE = {'time': 300, 'target': [420, 75, 810, 160], 'threshold': 0.98}
LIST_FREEZE = {'time': 300, 'target': OPERATOR_LIST, 'threshold': 0.98}


def operator_nodes():
    """Find an operator card by portrait (from the top of the list), click it and
    confirm the header name. Option cases fill the template and names."""
    find_next = ['CultivationOperatorSelected', 'CultivationOperatorFind']
    swipe = lambda begin, end: {'type': 'Swipe', 'param': {'begin': begin, 'end': end, 'duration': 500, 'end_hold': 200}}
    list_end = lambda: {'type': 'Custom', 'param': {'custom_recognition': 'ListCompleteRecognition',
                                                    'custom_recognition_param': {'threshold': 0.98}, 'roi': OPERATOR_LIST}}
    return {
        'CultivationOperatorSelected': node('DoNothing', ['CultivationBegin'], {
            'type': 'OCR', 'param': {'roi': OPERATOR_NAME, 'expected': ['(?!)']}}),
        'CultivationOperatorFind': node({'type': 'Click', 'param': {}}, ['CultivationOperatorSelected'], {
            'type': 'TemplateMatch', 'param': {'roi': OPERATOR_LIST, 'template': ['Cultivation/Operators/chr_0027_tangtang.png'], 'threshold': 0.8}},
            post_wait_freezes=OPERATOR_FREEZE),
        # Scroll to the top first: the target may sit above the current viewport.
        'CultivationOperatorTop': node('DoNothing', find_next + ['CultivationOperatorEnd', 'CultivationOperatorDown'],
                                       list_end(), attach={'ready': False}),
        'CultivationOperatorUp': node(swipe([200, 330], [200, 580]),
                                      find_next + ['CultivationOperatorTop', 'CultivationOperatorUp'],
                                      post_wait_freezes=LIST_FREEZE),
        'CultivationOperatorEnd': node(action('operator_missing'), [], list_end(), attach={'ready': False}),
        'CultivationOperatorDown': node(swipe([200, 580], [200, 330]),
                                        find_next + ['CultivationOperatorEnd', 'CultivationOperatorDown'],
                                        post_wait_freezes=LIST_FREEZE),
    }


def overview_nodes():
    """Reach 培养总览 from any screen: the upstream operator scene, then the
    培养总览 button at the top right of the operator page."""
    title = ['培养总览', '培養總覽']
    return {
        'CultivationScan': node('DoNothing', ['CultivationInOverview', 'CultivationOpenOverview',
                                              '[JumpBack]SceneEnterMenuOperator']),
        # Default (current operator): read whoever is selected in the overview.
        'CultivationInOverview': node('DoNothing', ['CultivationBegin'], {
            'type': 'OCR', 'param': {'roi': [35, 20, 200, 42], 'expected': title}}),
        'CultivationOverviewButton': node('DoNothing', recognition={
            'type': 'OCR', 'param': {'roi': [1020, 15, 180, 45], 'expected': title}}),
        'CultivationOpenOverview': node({'type': 'Click', 'param': {}}, ['CultivationInOverview'], {
            'type': 'And', 'param': {'all_of': ['InOperator', 'CultivationOverviewButton'], 'box_index': 1}},
            post_wait_freezes={'time': 500, 'target': [0, 0, 1280, 720], 'threshold': 0.98}),
    }


def build():
    pipeline = overview_nodes()
    pipeline['CultivationBegin'] = node(action('begin'), ['CultivationP0T0Select'])
    pipeline.update(operator_nodes())
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
                'begin': [1215, 380], 'end': [1215, 620], 'duration': 500, 'end_hold': 200}},
                [prefix+'Top'], guard(tab), post_wait_freezes=freeze)
            pipeline[prefix+'Read'] = node(action('read', **phase), [prefix+'DownSwipe'], guard(tab))
            pipeline[prefix+'DownSwipe'] = node({'type': 'Swipe', 'param': {
                'begin': [1215, 620], 'end': [1215, 380], 'duration': 500, 'end_hold': 200}},
                [prefix+'Bottom'], guard(tab), post_wait_freezes=freeze)
            pipeline[prefix+'Bottom'] = node(action('bottom', **phase, again=prefix+'Read', done=after),
                                             [prefix+'Read', after], guard(tab))
    pipeline['CultivationFinish'] = node(action('finish'), recognition=guard())
    for name, step in [('Preview', 'preview'), ('Export', 'export'), ('Clear', 'clear'), ('Confirm', 'confirm')]:
        pipeline['Cultivation'+name] = node(action(step))
    pipeline['CultivationOCR'] = node('DoNothing', recognition={
        'type': 'OCR', 'param': {'roi': [420, 75, 810, 590]}})
    pipeline['CultivationIcon'] = node('DoNothing', recognition={
        'type': 'Custom', 'param': {'custom_recognition': 'IconRecognition',
        'custom_recognition_param': {'grid_type': 'single_roi',
                                   'item_filters': ['ValuableDepot:SpecialItem', 'Normal:Nurturance', 'Isolate:Gold', 'ValuableDepot:CommercialItem']},
        'roi': [910, 386, 80, 80]}})
    write('assets/resource/pipeline/Cultivation/Cultivation.json', pipeline)
    tasks = []
    for name in ['Scan', 'Preview', 'Export', 'Clear', 'Confirm']:
        task = {'name': 'Cultivation'+name, 'label': '$task.Cultivation'+name+'.label',
                'entry': 'Cultivation'+name, 'group': ['other_menu'], 'controller': ['CloudADB']}
        if name == 'Scan':
            task['option'] = ['CultivationOperator', 'CultivationWeapon']
        elif name in ('Preview', 'Export'):
            task['option'] = ['CultivationWeapon']
        tasks.append(task)
    # An operator equips one weapon, so the user picks which usable weapon's
    # materials count. Keep leaves the stored choice untouched.
    cases = [{'name': 'Keep', 'label': '$option.CultivationWeapon.cases.Keep.label'}]
    for index, case in enumerate(['None', 'First', 'Second', 'Third', 'Fourth']):
        steps = {'CultivationFinish': 'finish', 'CultivationPreview': 'preview', 'CultivationExport': 'export'}
        cases.append({'name': case, 'label': f'$option.CultivationWeapon.cases.{case}.label', 'pipeline_override': {
            node: {'action': action(step, weapon=index)} for node, step in steps.items()}})
    options = {'CultivationWeapon': {'type': 'select', 'label': '$option.CultivationWeapon.label',
                                     'description': '$option.CultivationWeapon.description', 'cases': cases},
               'CultivationOperator': {'type': 'select', 'label': '$option.CultivationOperator.label',
                                       'description': '$option.CultivationOperator.description',
                                       'cases': operator_cases()}}
    write('assets/tasks/Cultivation.json', {'task': tasks, 'option': options})
    path = ROOT/'assets/interface.json'
    text = path.read_text(encoding='utf-8')
    if '"tasks/Cultivation.json"' not in text:
        text = text.replace('"tasks/PullCountCalculator.json",', '"tasks/PullCountCalculator.json",\n        "tasks/Cultivation.json",')
        path.write_text(text, encoding='utf-8', newline='\n')
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
        module_texts = {
            'zh_cn': ['培养任务进度', '生成模块接入预览',
                      '培养任务：%s / %s；材料已备齐 %d / %d 类。扫描时间：%s；计划修订：%d。',
                      '材料待补齐。以下是上次完整扫描的库存，运行模块后需重新核验。',
                      '上次扫描时材料已备齐；实际升级、突破、技能和武器培养结果待确认。',
                      '协议空间：目标已转换，待执行验收；采集：CloudADB目标库存待适配；基质：云端流程/武器词条待验证。尚未开始执行，不显示培养完成。',
                      '已导出 CultivationModules.preview.json，包含协议空间、采集、基质三模块的数据。此预览不会启动刷取或更新现有任务表单。',
                      '协议空间', '采集', '人工补充'],
            'zh_tw': ['培養任務進度', '產生模組接入預覽',
                      '培養任務：%s / %s；材料已備齊 %d / %d 類。掃描時間：%s；計畫修訂：%d。',
                      '材料待補齊。以下為上次完整掃描庫存，執行模組後需重新核驗。',
                      '上次掃描時材料已備齊；實際升級、突破、技能及武器培養結果待確認。',
                      '協議空間：目標已轉換，待執行驗收；採集：CloudADB目標庫存待適配；基質：雲端流程/武器詞條待驗證。尚未執行，不顯示培養完成。',
                      '已匯出 CultivationModules.preview.json，含協議空間、採集、基質三模組資料。預覽不啟動刷取或更新現有任務表單。',
                      '協議空間', '採集', '人工補充'],
            'en_us': ['Cultivation task progress', 'Export module integration preview',
                      'Cultivation: %s / %s; materials ready: %d / %d types. Scanned: %s; plan revision: %d.',
                      'Materials missing. Quantities are from the last complete scan; verify again after running modules.',
                      'Materials were ready at the last scan; actual upgrades, promotions, skills and weapon development remain unverified.',
                      'Protocol Space: goals converted, execution validation pending. Collection: CloudADB stock mode needs adaptation. Essence: cloud flow and weapon traits unverified. Execution has not started; cultivation is not complete.',
                      'CultivationModules.preview.json exported with Protocol Space, Collection and Essence data. This preview neither starts farming nor updates existing task forms.',
                      'Protocol Space', 'Collection', 'Manual'],
            'ja_jp': ['育成タスクの進捗', 'モジュール連携プレビューを出力',
                      '育成：%s / %s。素材準備済み %d / %d 種。読取日時：%s。計画版：%d。',
                      '素材が不足しています。数量は前回の全読取時点です。実行後は再確認が必要です。',
                      '前回読取時点で素材準備済み。レベル、昇進、スキル、武器の育成結果は未確認です。',
                      '協約空間：目標変換済み、実行検証待ち。採集：CloudADB在庫モードの対応待ち。基質：クラウド操作と武器特性は未確認。まだ実行しておらず、育成完了とは表示しません。',
                      'CultivationModules.preview.json に3モジュールのデータを出力しました。周回の開始や既存タスク設定の変更は行いません。',
                      '協約空間', '採集', '手動'],
            'ko_kr': ['육성 작업 진행 상황', '모듈 연결 미리 보기 내보내기',
                      '육성: %s / %s. 재료 준비 %d / %d종. 스캔: %s. 계획 버전: %d.',
                      '재료가 부족합니다. 수량은 마지막 전체 스캔 기준이며 실행 후 다시 확인해야 합니다.',
                      '마지막 스캔에서 재료가 준비되었습니다. 실제 레벨, 돌파, 스킬 및 무기 육성 결과는 미확인입니다.',
                      '프로토콜 공간: 목표 변환 완료, 실행 검증 대기. 채집: CloudADB 재고 모드 대응 필요. 기질: 클라우드 동작과 무기 특성 미확인. 아직 실행하지 않았으며 육성 완료로 표시하지 않습니다.',
                      '3개 모듈의 데이터를 CultivationModules.preview.json으로 내보냈습니다. 파밍을 시작하거나 기존 작업 설정을 변경하지 않습니다.',
                      '프로토콜 공간', '채집', '수동']
        }[lang]
        keys = ['task.CultivationPreview.label', 'task.CultivationExport.label',
                'cultivation.progress', 'cultivation.materials_missing', 'cultivation.materials_ready',
                'cultivation.modules_pending', 'cultivation.modules_exported',
                'cultivation.module.ProtocolSpace', 'cultivation.module.AutoCollect', 'cultivation.module.manual']
        data.update(zip(keys, module_texts))
        data['task.CultivationConfirm.label'], data['cultivation.user_confirmed'] = {
            'zh_cn': ['我已在游戏中完成培养（手动确认）', '培养任务已由用户确认完成：%s；确认时间：%s。尚无自动结果核验；以下材料为历史扫描。'],
            'zh_tw': ['我已在遊戲中完成培養（手動確認）', '培養任務已由使用者確認完成：%s；確認時間：%s。尚無自動結果核驗；以下為歷史掃描。'],
            'en_us': ['I completed cultivation in game (manual confirmation)', 'User confirmed cultivation complete: %s, at %s. Actual results have not been automatically verified; material counts below are historical.'],
            'ja_jp': ['ゲーム内の育成を完了した（手動確認）', 'ユーザーが育成完了を確認：%s。確認日時：%s。結果の自動検証はありません。以下は過去の素材読取です。'],
            'ko_kr': ['게임 내 육성을 완료했습니다 (수동 확인)', '사용자가 육성 완료 확인: %s, %s. 결과를 자동 검증하지 않았습니다. 아래 재료 수량은 과거 스캔 기준입니다.']
        }[lang]
        weapon_texts = {
            'zh_cn': ['培养武器', '干员只能装备一把武器。读取后日志会按游戏顺序列出可用武器，在此选择要计入刷取目标的那一把。',
                      '保持上次选择', '不培养武器', '第 1 把', '第 2 把', '第 3 把', '第 4 把'],
            'zh_tw': ['培養武器', '幹員只能裝備一把武器。讀取後日誌會依遊戲順序列出可用武器，在此選擇要計入刷取目標的那一把。',
                      '保持上次選擇', '不培養武器', '第 1 把', '第 2 把', '第 3 把', '第 4 把'],
            'en_us': ['Weapon to develop', 'An operator equips one weapon. After reading, the log lists the usable weapons in game order; choose which one counts toward farming goals.',
                      'Keep last choice', 'No weapon', 'Weapon 1', 'Weapon 2', 'Weapon 3', 'Weapon 4'],
            'ja_jp': ['育成する武器', 'オペレーターが装備できる武器は1つです。読み取り後、使用可能な武器がゲーム内の順にログへ表示されます。周回目標に含める武器を選択してください。',
                      '前回の選択を維持', '武器を育成しない', '1番目', '2番目', '3番目', '4番目'],
            'ko_kr': ['육성할 무기', '오퍼레이터는 무기를 하나만 장착합니다. 읽은 뒤 로그에 사용 가능 무기가 게임 순서대로 표시됩니다. 파밍 목표에 포함할 무기를 선택하세요.',
                      '이전 선택 유지', '무기 육성 안 함', '1번째', '2번째', '3번째', '4번째'],
        }[lang]
        data['option.CultivationWeapon.label'], data['option.CultivationWeapon.description'] = weapon_texts[:2]
        operator_texts = {
            'zh_cn': ['干员', '自动进入培养总览并选中该干员后读取；「当前已选」读取培养总览里已经选中的干员。', '当前已选'],
            'zh_tw': ['幹員', '自動進入培養總覽並選中該幹員後讀取；「目前已選」讀取培養總覽中已選中的幹員。', '目前已選'],
            'en_us': ['Operator', 'Opens the cultivation overview and selects this operator before reading; "Current" reads the operator already selected there.', 'Current'],
            'ja_jp': ['オペレーター', '育成一覧を開いてこのオペレーターを選択してから読み取ります。「現在の選択」は育成一覧で選択中のオペレーターを読み取ります。', '現在の選択'],
            'ko_kr': ['오퍼레이터', '육성 개요를 열고 이 오퍼레이터를 선택한 뒤 읽습니다. "현재 선택"은 육성 개요에서 이미 선택한 오퍼레이터를 읽습니다.', '현재 선택'],
        }[lang]
        data['option.CultivationOperator.label'], data['option.CultivationOperator.description'], \
            data['option.CultivationOperator.cases.Current.label'] = operator_texts
        for case, label in zip(['Keep', 'None', 'First', 'Second', 'Third', 'Fourth'], weapon_texts[2:]):
            data[f'option.CultivationWeapon.cases.{case}.label'] = label
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
        path.write_text(original, encoding='utf-8', newline='\n')
    write('tools/schema/components/cultivation.schema.json', {'$schema': 'https://json-schema.org/draft/2020-12/schema', '$defs': {
        'Action': {'type': 'object', 'required': ['step'], 'additionalProperties': False, 'properties': {
            'step': {'enum': ['begin', 'reset', 'top', 'read', 'bottom', 'finish', 'preview', 'export', 'clear', 'confirm', 'operator_missing']},
            'tab': {'type': 'integer', 'minimum': 0, 'maximum': 3},
            'pass': {'type': 'integer', 'minimum': 0, 'maximum': 1},
            'again': {'type': 'string', 'pattern': '^Cultivation'}, 'done': {'type': 'string', 'pattern': '^Cultivation'},
            'weapon': {'type': 'integer', 'minimum': 0, 'maximum': 4}}},
        'Guard': {'type': 'object', 'additionalProperties': False,
                  'properties': {'tab': {'type': 'integer', 'minimum': 0, 'maximum': 3}}}}})
    print(f'Generated {len(pipeline)} native nodes, five tasks, five locale entries, and Custom schemas.')


def operator_cases():
    """One case per operator from zmdmap, labelled with the existing operator.<Name>
    locale key, matched by the generated portrait and confirmed by name OCR."""
    operators = json.loads((ROOT/'tools/zmdmap/data/sell_product.json').read_text(encoding='utf-8'))['operators']
    labels = json.loads((ROOT/'assets/locales/interface/zh_cn.json').read_text(encoding='utf-8'))
    key_by_name = {v: k for k, v in labels.items() if k.startswith('operator.')}
    grouped = {}
    for char_id, operator in operators.items():
        key = key_by_name.get(operator['names']['zh_cn'])
        if key is None:
            raise ValueError(f'{char_id}: no operator.<Name> locale key; add the name before generating')
        if not (ROOT/f'assets/resource_adb/image/Cultivation/Operators/{char_id}.png').exists():
            raise ValueError(f'{char_id}: missing portrait; run tools.Cultivation.build_operator_templates first')
        entry = grouped.setdefault(key, {'templates': [], 'names': []})
        entry['templates'].append(f'Cultivation/Operators/{char_id}.png')
        for name in operator['names'].values():
            pattern = '^' + re.escape(name) + '$'
            if pattern not in entry['names']:
                entry['names'].append(pattern)
    cases = [{'name': 'Current', 'label': '$option.CultivationOperator.cases.Current.label'}]
    for key, entry in grouped.items():
        cases.append({'name': key[len('operator.'):], 'label': '$' + key, 'pipeline_override': {
            'CultivationInOverview': {'action': {'type': 'Custom', 'param': {
                'custom_action': 'PipelineOverrideAction', 'custom_action_param': {'patch': {
                    'CultivationOperatorTop': {'attach': {'ready': False}},
                    'CultivationOperatorEnd': {'attach': {'ready': False}}}}}},
                'next': ['CultivationOperatorSelected', 'CultivationOperatorFind', 'CultivationOperatorTop', 'CultivationOperatorUp']},
            'CultivationOperatorSelected': {'recognition': {'type': 'OCR', 'param': {'roi': OPERATOR_NAME, 'expected': entry['names']}}},
            'CultivationOperatorFind': {'recognition': {'type': 'TemplateMatch', 'param': {
                'roi': OPERATOR_LIST, 'template': entry['templates'], 'threshold': 0.8}}}}})
    return cases


if __name__ == '__main__':
    build()
