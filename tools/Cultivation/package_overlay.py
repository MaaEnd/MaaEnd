"""Package a version-bound overlay; never install it or launch the client."""
import hashlib
import json
from pathlib import Path
import shutil
import zipfile

ROOT = Path(__file__).resolve().parents[2]
OUT = ROOT.parents[1]/'artifacts/native-cultivation-20261020'


def main():
    overlay = OUT/'overlay'
    entries = ['assets/interface.json', 'assets/tasks/Cultivation.json',
               'assets/resource/pipeline/Cultivation/Cultivation.json']
    entries += [f'assets/locales/interface/{lang}.json' for lang in ['zh_cn','zh_tw','en_us','ja_jp','ko_kr']]
    for entry in entries:
        source=ROOT/entry
        target=overlay/Path(entry).relative_to('assets')
        target.parent.mkdir(parents=True,exist_ok=True)
        shutil.copyfile(source,target)
    # Preserve release identity while marking the fork as an explicit prototype.
    interface=overlay/'interface.json'
    text=interface.read_text(encoding='utf-8').replace('"version": "v0.1.0"','"version": "v2.35.0-beta.1-native-cultivation-prototype.1"')
    interface.write_text(text,encoding='utf-8')
    (overlay/'agent').mkdir(exist_ok=True)
    shutil.copyfile(OUT/'go-service.exe',overlay/'agent/go-service.exe')
    for lang in ['zh_cn','en_us']:
        shutil.copyfile(ROOT/f'docs/{lang}/users/cultivation-prototype.md',OUT/f'USER_GUIDE_{lang}.md')
    manifest=[]
    for path in sorted(overlay.rglob('*')):
        if path.is_file():manifest.append({'path':path.relative_to(overlay).as_posix(),'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'size':path.stat().st_size})
    (OUT/'overlay_SHA256.json').write_text(json.dumps({'upstream':'v2.35.0-beta.1','prototype_only':True,'device_accepted':False,'files':manifest},ensure_ascii=False,indent=2),encoding='utf-8')
    archive=OUT/'MaaEnd-native-cultivation-prototype.1.zip'
    with zipfile.ZipFile(archive,'w',zipfile.ZIP_DEFLATED) as z:
        for path in sorted(overlay.rglob('*')):
            if path.is_file():z.write(path,path.relative_to(OUT))
        for name in ['USER_GUIDE_zh_cn.md','USER_GUIDE_en_us.md','overlay_SHA256.json']:
            z.write(OUT/name,name)
    print(f'Packaged {len(manifest)} overlay files: {archive} ({archive.stat().st_size} bytes). No installation performed.')


if __name__=='__main__':main()
