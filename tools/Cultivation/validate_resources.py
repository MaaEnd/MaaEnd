"""Validate this prototype's schemas and references without MaaFramework calls."""
import json
from pathlib import Path
import sys
import jsonschema
from referencing import Registry, Resource

ROOT = Path(__file__).resolve().parents[2]
SCHEMAS = ROOT/'tools/schema'


def main():
    resources = []
    for path in SCHEMAS.rglob('*.json'):
        try:
            data = json.loads(path.read_text(encoding='utf-8-sig'))
        except ValueError:
            continue
        resources.append((path.as_uri(), Resource.from_contents(data, default_specification=__import__('referencing.jsonschema', fromlist=['DRAFT202012']).DRAFT202012)))
    registry = Registry().with_resources(resources)
    schema = json.loads((SCHEMAS/'pipeline.schema.json').read_text(encoding='utf-8'))
    validator = jsonschema.Draft202012Validator({'$ref': (SCHEMAS/'pipeline.schema.json').as_uri()}, registry=registry)
    pipeline = json.loads((ROOT/'assets/resource/pipeline/Cultivation/Cultivation.json').read_text(encoding='utf-8'))
    errors = sorted(validator.iter_errors(pipeline), key=lambda error: str(error.path))
    for error in errors:
        print('/'.join(map(str,error.path)), error.message)
    if errors:
        return 1
    for name,node in pipeline.items():
        for target in node.get('next',[]):
            assert target in pipeline, (name,target)
        assert node.get('pre_delay')==0 and node.get('post_delay')==0 and node.get('rate_limit')==0
        assert 'focus' not in node
    tasks = json.loads((ROOT/'assets/tasks/Cultivation.json').read_text(encoding='utf-8'))['task']
    assert len(tasks)==4
    for task in tasks:
        assert task['entry'] in pipeline
        for path in (ROOT/'assets/locales/interface').glob('*.json'):
            locale = json.loads(path.read_text(encoding='utf-8'))
            assert task['label'][1:] in locale, (path,task['label'])
    print(f'PASS: {len(pipeline)} Pipeline nodes/schema/references; 4 task entries and 5 locales; no device calls.')
    return 0


if __name__=='__main__':
    sys.exit(main())
