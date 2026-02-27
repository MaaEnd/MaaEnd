#!/usr/bin/env python3
"""
Export Endfield_Vision_Models EF_items_11n.pt to ONNX and copy to
assets/resource/model/detect/ItemTransfer/ItemTransfer.onnx.
Requires: pip install ultralytics

Usage:
  python tools/ItemTransfer/export_ef_items_onnx.py
  python tools/ItemTransfer/export_ef_items_onnx.py /path/to/EF_items_11n.pt
"""
import os
import shutil
import ssl
import sys
import urllib.request

RELEASE_URL = "https://github.com/icaruszezen/Endfield_Vision_Models/releases/download/v1.0.0/EF_items_11n.pt"
SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(os.path.dirname(SCRIPT_DIR))
OUT_DIR = os.path.join(ROOT, "assets", "resource", "model", "detect", "ItemTransfer")
PT_DEFAULT = os.path.join(SCRIPT_DIR, "EF_items_11n.pt")
ONNX_NAME = "ItemTransfer.onnx"


def main():
    os.makedirs(OUT_DIR, exist_ok=True)
    if len(sys.argv) >= 2:
        PT_PATH = os.path.abspath(sys.argv[1])
        if not os.path.isfile(PT_PATH):
            print("File not found:", PT_PATH, file=sys.stderr)
            return 1
    else:
        PT_PATH = PT_DEFAULT
        if not os.path.isfile(PT_PATH):
            print("Downloading EF_items_11n.pt ...")
            try:
                ctx = ssl.create_default_context()
                urllib.request.urlretrieve(RELEASE_URL, PT_PATH, context=ctx)
            except urllib.error.URLError as e:
                print("Download failed:", e, file=sys.stderr)
                print("Please download EF_items_11n.pt from", RELEASE_URL, file=sys.stderr)
                print("Then run: python tools/ItemTransfer/export_ef_items_onnx.py <path/to/EF_items_11n.pt>", file=sys.stderr)
                return 1
            print("Downloaded to", PT_PATH)
    print("Using", PT_PATH)
    print("Exporting to ONNX (imgsz=640, opset=17) ...")
    from ultralytics import YOLO
    model = YOLO(PT_PATH)
    model.export(format="onnx", imgsz=640, opset=17)
    onnx_src = PT_PATH.replace(".pt", ".onnx")
    if not os.path.isfile(onnx_src):
        raise SystemExit("Export failed: no .onnx produced")
    onnx_dst = os.path.join(OUT_DIR, ONNX_NAME)
    shutil.copy2(onnx_src, onnx_dst)
    print("Saved to", onnx_dst)
    return 0


if __name__ == "__main__":
    sys.exit(main())
