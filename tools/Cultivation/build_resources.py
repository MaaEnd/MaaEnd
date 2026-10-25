"""Generate the Cultivation red-label reader pipeline; no device or SRC imports.

The reader has no task of its own: SupplyPlan overrides the operator nodes at runtime."""
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
# Every way the reader ends continues here; SupplyPlan defines this node.
RED_CHECK_NEXT = 'SupplyPlanRedCheckNext'
# Tab centres of the two operator material tabs; the weapon tab is not read.
TAB_X = [568, 750]


def write(path, data):
    target = ROOT / path
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(json.dumps(data, ensure_ascii=False, indent=4) + '\n', encoding='utf-8', newline='\n')


def action(step, **kwargs):
    return {'type': 'Custom', 'param': {'custom_action': 'CultivationAction',
            'custom_action_param': {'step': step, **kwargs}}}


def guard(tab=None, decision=None):
    params = {} if tab is None else {'tab': tab}
    if decision is not None:
        params['decision'] = decision
    return {'type': 'Custom', 'param': {'custom_recognition': 'CultivationGuard',
            'custom_recognition_param': params, 'roi': [420, 75, 810, 160]}}


def node(operation, next_nodes=None, recognition='DirectHit', **extra):
    return {'recognition': recognition, 'action': operation, 'pre_delay': 0,
            'post_delay': 0, 'rate_limit': 0, 'next': next_nodes or [], **extra}


# Left operator list and the selected operator's name in the overview header.
OPERATOR_LIST = [30, 100, 350, 520]
OPERATOR_NAME = [480, 85, 300, 40]
OPERATOR_FREEZE = {'time': 300, 'target': [420, 75, 810, 160], 'threshold': 0.98}
LIST_FREEZE = {'time': 300, 'target': OPERATOR_LIST, 'threshold': 0.98}


# 培养期望 sets the target levels, so the material gap differs per level; the
# chosen level is applied before reading. Rows of the selection panel, top to bottom.
EXPECTATIONS = {'Basic': ['培养期望：基础'], 'Advanced': ['培养期望：进阶'], 'Proficient': ['培养期望：精通'], 'Excellent': ['培养期望：卓越']}
EXPECTATION_HEADER = [990, 110, 180, 40]
EXPECTATION_PANEL = [860, 265, 340, 195]
EXPECTATION_NEXT = ['CultivationExpectationReady', 'CultivationExpectationOpen']


def expectation_nodes():
    """Check the header level; otherwise open the panel, click the level and
    confirm the header again. The level can be overridden at runtime."""
    freeze = {'time': 300, 'target': [420, 75, 810, 400], 'threshold': 0.98}
    level = EXPECTATIONS['Excellent']
    return {
        'CultivationExpectationReady': node('DoNothing', ['CultivationBegin'], {
            'type': 'OCR', 'param': {'roi': EXPECTATION_HEADER, 'expected': level}}),
        'CultivationExpectationOpen': node({'type': 'Click', 'param': {}}, ['CultivationExpectationPick', 'CultivationExpectationMissing'], {
            'type': 'OCR', 'param': {'roi': EXPECTATION_HEADER, 'expected': [text for level in EXPECTATIONS.values() for text in level]}}, post_wait_freezes=freeze),
        'CultivationExpectationPick': node({'type': 'Click', 'param': {}}, ['CultivationExpectationReady', 'CultivationExpectationMissing'], {
            'type': 'OCR', 'param': {'roi': EXPECTATION_PANEL, 'expected': level}}, post_wait_freezes=freeze),
        'CultivationExpectationMissing': node(action('expectation_missing'), [RED_CHECK_NEXT]),
    }


def operator_nodes():
    """Find an operator card by portrait (from the top of the list), click it and
    confirm the header name. The template and names are overridden at runtime."""
    find_next = ['CultivationOperatorSelected', 'CultivationOperatorFind']
    def swipe(begin, end):
        return {'type': 'Swipe', 'param': {'begin': begin, 'end': end, 'duration': 500, 'end_hold': 200}}
    def list_end():
        return {'type': 'Custom', 'param': {'custom_recognition': 'ListCompleteRecognition',
                                                        'custom_recognition_param': {'threshold': 0.98}, 'roi': OPERATOR_LIST}}
    return {
        # Verify the overview and its expectation header before searching or
        # moving the portrait list; a popup/scene change must stop the search.
        'CultivationOperatorListReady': node('DoNothing', recognition={
            'type': 'And', 'param': {'all_of': ['CultivationInOverview', 'CultivationExpectationOpen']}}),
        'CultivationOperatorSelected': node('DoNothing', EXPECTATION_NEXT, {
            'type': 'OCR', 'param': {'roi': OPERATOR_NAME}}, enabled=False),
        # The clicked portrait is confirmed by name, without a second click.
        # A stale first header must not select a DirectHit failure branch.
        'CultivationOperatorPortrait': node('DoNothing', recognition={
            'type': 'TemplateMatch', 'param': {'roi': OPERATOR_LIST, 'template': ['Cultivation/Operators/chr_0027_tangtang.png'], 'threshold': 0.8}},
            enabled=False),
        'CultivationOperatorFind': node({'type': 'Click', 'param': {}}, ['CultivationOperatorSelected'], {
            'type': 'And', 'param': {'all_of': ['CultivationOperatorListReady', 'CultivationOperatorPortrait'], 'box_index': 1}},
            post_wait_freezes=OPERATOR_FREEZE, on_error=['CultivationOperatorMissing']),
        # Scroll to the top first: the target may sit above the current viewport.
        'CultivationOperatorCheckUp': node('DoNothing', [*find_next, 'CultivationOperatorTop', 'CultivationOperatorUp'],
                                           {'type': 'And', 'param': {'all_of': ['CultivationOperatorListReady']}}),
        'CultivationOperatorTop': node('DoNothing', ['CultivationOperatorCheckDown'],
                                       list_end(), attach={'ready': False}),
        'CultivationOperatorUp': node(swipe([200, 330], [200, 580]),
                                      ['CultivationOperatorCheckUp'],
                                      {'type': 'And', 'param': {'all_of': ['CultivationOperatorListReady']}},
                                      post_wait_freezes=LIST_FREEZE),
        # End of the portrait search: report failure instead of clicking every card.
        'CultivationOperatorEnd': node(action('operator_missing'), [RED_CHECK_NEXT], list_end(), attach={'ready': False}),
        'CultivationOperatorCheckDown': node('DoNothing', [*find_next, 'CultivationOperatorEnd', 'CultivationOperatorDown'],
                                             {'type': 'And', 'param': {'all_of': ['CultivationOperatorListReady']}}),
        'CultivationOperatorDown': node(swipe([200, 580], [200, 330]),
                                        ['CultivationOperatorCheckDown'],
                                        {'type': 'And', 'param': {'all_of': ['CultivationOperatorListReady']}},
                                        post_wait_freezes=LIST_FREEZE),
        'CultivationOperatorMissing': node(action('operator_missing'), [RED_CHECK_NEXT]),
    }


def overview_nodes():
    """Reach 培养总览 from any screen: the upstream operator scene, then the
    培养总览 button at the top right of the operator page."""
    title = ['培养总览']
    return {
        'CultivationScan': node('DoNothing', ['CultivationInOverview', 'CultivationOpenOverview',
                                              '[JumpBack]SceneEnterMenuOperator']),
        # Default (current operator): read whoever is selected in the overview.
        'CultivationInOverview': node('DoNothing', EXPECTATION_NEXT, {
            'type': 'OCR', 'param': {'roi': [35, 20, 200, 42], 'expected': title}}),
        'CultivationOverviewButton': node('DoNothing', recognition={
            'type': 'OCR', 'param': {'roi': [1020, 15, 180, 45], 'expected': title}}),
        'CultivationOpenOverview': node({'type': 'Click', 'param': {}}, ['CultivationInOverview'], {
            'type': 'And', 'param': {'all_of': ['InOperator', 'CultivationOverviewButton'], 'box_index': 1}},
            post_wait_freezes={'time': 500, 'target': [0, 0, 1280, 720], 'threshold': 0.98}),
    }



def build():
    """Generate one fresh-read pass over the two operator material tabs."""
    pipeline = overview_nodes()
    pipeline.update(operator_nodes())
    pipeline.update(expectation_nodes())
    # A non-CloudADB run is skipped by Go; every other begin continues to the tabs.
    pipeline['CultivationBegin'] = node(action('begin'), ['CultivationSkipped', 'CultivationT0Select'])
    for tab, x in enumerate(TAB_X):
        prefix = f'CultivationT{tab}'
        after = f'CultivationT{tab+1}Select' if tab + 1 < len(TAB_X) else 'CultivationFinish'
        # List rows keep settling for a moment after a swipe (snap and row
        # height); capturing too early makes scroll alignment unmeasurable.
        freeze = {'time': 900, 'target': [425, 250, 813, 415], 'threshold': 0.98}
        pipeline[prefix+'Select'] = node({'type': 'Click', 'param': {'target': [x, 207]}},
                                         [prefix+'Reset'], guard(), post_wait_freezes=freeze)
        pipeline[prefix+'Reset'] = node(action('reset', tab=tab), [prefix+'Top'], guard(tab))
        pipeline[prefix+'Top'] = node(action('top', tab=tab),
                                      [prefix+'AtTop', prefix+'AboveTop'], guard(tab))
        pipeline[prefix+'AtTop'] = node('DoNothing', [prefix+'Read'], guard(tab, 'done'))
        pipeline[prefix+'AboveTop'] = node('DoNothing', [prefix+'TopSwipe'], guard(tab, 'again'))
        pipeline[prefix+'TopSwipe'] = node({'type': 'Swipe', 'param': {
            'begin': [1215, 380], 'end': [1215, 620], 'duration': 500, 'end_hold': 200}},
            [prefix+'Top'], guard(tab), post_wait_freezes=freeze)
        pipeline[prefix+'Read'] = node(action('read', tab=tab), [prefix+'DownSwipe'], guard(tab))
        # Keep the previously measured overlap; Go checks actual displacement.
        pipeline[prefix+'DownSwipe'] = node({'type': 'Swipe', 'param': {
            'begin': [1215, 620], 'end': [1215, 460], 'duration': 500, 'end_hold': 200}},
            [prefix+'Bottom'], guard(tab), post_wait_freezes=freeze)
        pipeline[prefix+'Bottom'] = node(action('bottom', tab=tab),
                                         [prefix+'AtBottom', prefix+'BelowPage'], guard(tab))
        pipeline[prefix+'AtBottom'] = node('DoNothing', [after], guard(tab, 'done'))
        pipeline[prefix+'BelowPage'] = node('DoNothing', [prefix+'Read'], guard(tab, 'again'))
    pipeline['CultivationFinish'] = node(action('finish'), ['CultivationDone'], guard())
    pipeline['CultivationDone'] = node(action('end'), [RED_CHECK_NEXT], guard(decision='done'))
    pipeline['CultivationSkipped'] = node(action('end'), [RED_CHECK_NEXT], guard(decision='skip'))
    pipeline['CultivationOCR'] = node('DoNothing', recognition={
        'type': 'OCR', 'param': {'roi': [420, 75, 810, 590]}})
    pipeline['CultivationIcon'] = node('DoNothing', recognition={
        'type': 'Custom', 'param': {'custom_recognition': 'IconRecognition',
            'custom_recognition_param': {'grid_type': 'single_roi'}, 'roi': [910, 386, 80, 80]}})
    write('assets/resource/pipeline/Cultivation/Cultivation.json', pipeline)
    write('tools/schema/components/cultivation.schema.json', {
        '$schema': 'https://json-schema.org/draft/2020-12/schema', '$defs': {
            'Action': {'type': 'object', 'required': ['step'], 'additionalProperties': False, 'properties': {
                'step': {'enum': ['begin', 'reset', 'top', 'read', 'bottom', 'finish', 'end', 'operator_missing', 'expectation_missing']},
                'tab': {'type': 'integer', 'minimum': 0, 'maximum': len(TAB_X) - 1}}},
            'Guard': {'type': 'object', 'additionalProperties': False, 'properties': {
                'decision': {'enum': ['again', 'done', 'skip']},
                'tab': {'type': 'integer', 'minimum': 0, 'maximum': len(TAB_X) - 1}}}}})
    update_locales()
    print(f'Generated {len(pipeline)} nodes and Custom schemas.')


# Finished UI strings only; no generated option descriptions or focus fields.
# Order: failed, operator_missing, expectation_missing, finish.
LOCALES = {
    'zh_cn': ['培养读取失败，请核对页面和日志。本轮不会沿用旧计划。', '未确认所选干员，请核对选择与页面状态。',
              '培养期望未确认，本轮跳过该干员。', '%s：本次识别到 %d 类缺料；只读取红字，不保存库存。'],
    'zh_tw': ['培養讀取失敗，請核對畫面和日誌。本輪不沿用舊計畫。', '未確認所選幹員，請核對選擇與畫面狀態。',
              '培養期望未確認，本輪跳過該幹員。', '%s：本次辨識到 %d 類缺料；僅讀紅字，不儲存庫存。'],
    'en_us': ['Cultivation reading failed. Check the screen and logs; no previous plan is reused.', 'Could not confirm the selected operator. Check your selection and the current screen.',
              'Cultivation goal could not be confirmed; this operator is skipped.', '%s: %d missing-material routes found. Only red labels are read; no inventory is saved.'],
    'ja_jp': ['育成の読み取りに失敗しました。画面とログを確認してください。以前の計画は使用しません。', '選択したオペレーターを確認できません。選択内容と画面の状態を確認してください。',
              '育成目標を確認できないため、このオペレーターをスキップしました。', '%s：不足素材は%d種です。赤字のみ読み取り、在庫は保存しません。'],
    'ko_kr': ['육성 읽기에 실패했습니다. 화면과 로그를 확인하세요. 이전 계획을 재사용하지 않습니다.', '선택한 오퍼레이터를 확인하지 못했습니다. 선택과 현재 화면을 확인하세요.',
              '육성 목표를 확인하지 못하여 이 오퍼레이터를 건너뜁니다.', '%s: 부족 재료 경로 %d종을 찾았습니다. 빨간 글자만 읽으며 재고는 저장하지 않습니다.'],
}


def update_locales():
    """Keep the Go-service text of all five languages in sync."""
    for lang, texts in LOCALES.items():
        path = f'assets/locales/go-service/{lang}.json'
        data = json.loads((ROOT/path).read_text(encoding='utf-8'))
        data = {k: v for k, v in data.items() if not k.startswith('cultivation.')}
        for key, value in zip(['failed', 'operator_missing', 'expectation_missing', 'finish'], texts, strict=True):
            data['cultivation.'+key] = value
        write(path, data)


if __name__ == '__main__':
    build()
