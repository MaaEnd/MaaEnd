#!/usr/bin/env python3
"""
Build item name -> Endfield Vision class id from classes.txt and update
ItemTransfer task JSON to use expected (class id) for NeuralNetworkDetect.
Class id = 0-based line number in items/classes.txt.

Usage:
  python tools/ItemTransfer/item_transfer_expected_ids.py
  python tools/ItemTransfer/item_transfer_expected_ids.py /path/to/classes.txt
"""
import json
import os
import sys

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(os.path.dirname(SCRIPT_DIR))
CLASSES_PATH = os.path.join(
    ROOT, "..", "..", "icaruszezen", "Endfield_Vision_Models", "items", "classes.txt"
)
if len(sys.argv) > 1:
    CLASSES_PATH = os.path.abspath(sys.argv[1])
TASK_PATH = os.path.join(ROOT, "assets", "tasks", "ItemTransfer.json")


def load_name_to_id(classes_path):
    with open(classes_path, "r", encoding="utf-8") as f:
        lines = [line.strip() for line in f if line.strip()]
    return {name: i for i, name in enumerate(lines)}


def main():
    if not os.path.isfile(CLASSES_PATH):
        print("Classes file not found:", CLASSES_PATH, file=sys.stderr)
        print("Usage: python tools/ItemTransfer/item_transfer_expected_ids.py [path/to/classes.txt]", file=sys.stderr)
        sys.exit(1)
    name_to_id = load_name_to_id(CLASSES_PATH)
    with open(TASK_PATH, "r", encoding="utf-8") as f:
        task = json.load(f)
    cases = task["option"]["WhatToTransfer"]["cases"]
    missing = []
    for case in cases:
        name = case["name"]
        if name not in name_to_id:
            missing.append(name)
            continue
        cid = name_to_id[name]
        overrides = case["pipeline_override"]
        for node in ("ItemTransferFindItemInRepo", "ItemTransferFindItemInBag", "ItemTransferFindItemInBagReturn"):
            if node in overrides:
                overrides[node] = {"expected": cid}
    if missing:
        print("Items not in model classes (left as template):", missing, file=sys.stderr)
    with open(TASK_PATH, "w", encoding="utf-8") as f:
        json.dump(task, f, ensure_ascii=False, indent=4)
    print("Updated", TASK_PATH)
    return 0


if __name__ == "__main__":
    sys.exit(main())
