#!/usr/bin/env python3
import json
import sys
import re
from pathlib import Path
from jsonschema import Draft7Validator, Draft202012Validator, RefResolver
from jsonschema.exceptions import ValidationError

def strip_jsonc_comments(text):
    """
    移除 JSONC 注释，保持 JSON 结构完整
    """
    # 状态机：0=正常, 1=字符串中, 2=转义字符
    result = []
    state = 0
    i = 0

    while i < len(text):
        char = text[i]

        if state == 0:  # 正常状态
            if char == '"':
                result.append(char)
                state = 1
                i += 1
            elif i + 1 < len(text) and text[i:i+2] == '//':
                # 单行注释，跳到行尾
                while i < len(text) and text[i] != '\n':
                    i += 1
                if i < len(text):
                    result.append('\n')  # 保留换行
                    i += 1
            elif i + 1 < len(text) and text[i:i+2] == '/*':
                # 多行注释，跳到 */
                i += 2
                while i + 1 < len(text) and text[i:i+2] != '*/':
                    if text[i] == '\n':
                        result.append('\n')  # 保留换行以维持行号
                    i += 1
                i += 2  # 跳过 */
            else:
                result.append(char)
                i += 1
        elif state == 1:  # 字符串中
            result.append(char)
            if char == '\\':
                state = 2
            elif char == '"':
                state = 0
            i += 1
        elif state == 2:  # 转义字符
            result.append(char)
            state = 1
            i += 1

    return ''.join(result)

def load_jsonc(file_path):
    """加载 JSONC 文件"""
    with open(file_path, 'r', encoding='utf-8') as f:
        content = f.read()

    # 移除注释
    clean_content = strip_jsonc_comments(content)

    try:
        return json.loads(clean_content)
    except json.JSONDecodeError as e:
        print(f"JSON decode error in {file_path}: {e}")
        # 调试：保存清理后的内容
        debug_file = f"/tmp/debug_{Path(file_path).name}"
        with open(debug_file, 'w') as f:
            f.write(clean_content)
        print(f"Cleaned content saved to {debug_file}")
        raise

def get_validator_class(schema):
    """根据 schema 的 $schema 字段选择合适的验证器"""
    schema_uri = schema.get('$schema', '')

    if 'draft-07' in schema_uri or 'draft/07' in schema_uri:
        return Draft7Validator
    elif '2020-12' in schema_uri:
        return Draft202012Validator
    else:
        # 默认使用 2020-12
        return Draft202012Validator

def validate_file(schema_path, file_path, resolver):
    """验证单个文件"""
    try:
        schema = load_jsonc(schema_path)
        data = load_jsonc(file_path)

        ValidatorClass = get_validator_class(schema)
        validator = ValidatorClass(schema, resolver=resolver)
        errors = list(validator.iter_errors(data))

        if errors:
            print(f"\n❌ Validation failed for {file_path}:")
            print(f"   Found {len(errors)} error(s):")
            for idx, error in enumerate(errors[:10], 1):
                path = '/' + '/'.join(str(p) for p in error.path) if error.path else '/'
                print(f"   {idx}. {path}: {error.message}")
            return False

        print(f"✓ {file_path}")
        return True
    except Exception as e:
        print(f"\n❌ Error validating {file_path}: {e}")
        return False

def main():
    all_valid = True

    # 创建 resolver 来处理 $ref
    schema_dir = Path("tools/schema").resolve()
    store = {}

    # 加载所有 schema 文件到 store
    for schema_file in schema_dir.glob("*.json"):
        try:
            schema = load_jsonc(schema_file)
            # 使用多种格式的 URI 作为 key
            file_uri = schema_file.as_uri()
            relative_path = f"./{schema_file.name}"
            absolute_path = f"/{schema_file.name}"

            store[file_uri] = schema
            store[relative_path] = schema
            store[absolute_path] = schema
        except Exception as e:
            print(f"Warning: Failed to load schema {schema_file}: {e}")

    pipeline_schema = load_jsonc("tools/schema/pipeline.schema.json")
    pipeline_schema_uri = (schema_dir / "pipeline.schema.json").as_uri()
    resolver = RefResolver(base_uri=pipeline_schema_uri, referrer=pipeline_schema, store=store)

    print("Validating pipeline resources...")
    # 验证 pipeline 资源文件
    for file_path in Path("assets/resource").rglob("*.json"):
        if not validate_file("tools/schema/pipeline.schema.json", file_path, resolver):
            all_valid = False

    for file_path in Path("assets/resource").rglob("*.jsonc"):
        if not validate_file("tools/schema/pipeline.schema.json", file_path, resolver):
            all_valid = False

    print("\nValidating interface files...")
    # 验证 interface 文件
    if Path("assets/interface.json").exists():
        interface_schema = load_jsonc("tools/schema/interface.schema.json")
        interface_resolver = RefResolver.from_schema(interface_schema)
        if not validate_file("tools/schema/interface.schema.json", "assets/interface.json", interface_resolver):
            all_valid = False

    if all_valid:
        print("\n✅ All validations passed!")
        sys.exit(0)
    else:
        print("\n❌ Some validations failed!")
        sys.exit(1)

if __name__ == "__main__":
    main()
