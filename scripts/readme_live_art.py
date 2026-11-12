#!/usr/bin/env python3
"""Package screenshots of the installed ArcMD client as self-contained SVGs.

Capture the named PNGs with the native app first; this script never launches
ArcMD or renders a development build. ImageMagick only crops and downsamples
the captures. The small, explicitly labeled material comparison is vector art.
"""

import argparse
import base64
import hashlib
import html
import json
from pathlib import Path
import subprocess


ROOT = Path(__file__).resolve().parents[1]
FONT = '-apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC",sans-serif'
THEMES = [
    ("theme-01-pink.png", ["#F29BBB"], .63, "换一种颜色", "玫瑰粉，让空间柔和下来。", "Pick a color", "A soft rose for your space."),
    ("theme-02-orange.png", ["#FF8558"], .63, "换一种颜色", "换成日落橙，整个外壳即时变化。", "Pick a color", "Sunset orange, across the whole shell."),
    ("theme-03-blue.png", ["#6CBAD9"], .63, "换一种颜色", "再换湖蓝，找到此刻的心情。", "Pick a color", "Then lake blue. A different mood."),
    ("theme-04-duo.png", ["#6CBAD9", "#7F7EDC"], 1, "两种颜色，开始交融", "点一下 +，蓝色向紫色过渡。", "Two colors blend", "Add a second color: blue into violet."),
    ("theme-05-trio.png", ["#6CBAD9", "#7F7EDC", "#8FE6BB"], 1, "三种颜色，一个空间", "再加薄荷绿，让渐变铺满窗口。", "Three colors, one space", "Add mint. The gradient fills the shell."),
    ("theme-07-strong.png", ["#6CBAD9"], .74, "调节透明感", "提高强度，颜色覆盖更明显。", "Adjust translucency", "Raise intensity for a stronger tint."),
    ("theme-08-transparent.png", ["#6CBAD9"], .19, "让背景透出来", "降低强度，背景材质更明显。", "Let the background through", "Lower intensity to reveal the material."),
    ("theme-11-dark.png", ["#334552", "#39364F", "#30483F"], .85, "从浅色，到深色", "正文纸面与空间一起切换。", "From light to dark", "The document follows the space."),
]
BOOKMARKS = [
    ("bookmark-01-hover.png", "停在想留下的段落", "页边出现书签入口。", "Pause on a paragraph", "A bookmark appears in the margin."),
    ("bookmark-02-added.png", "点一下，记住这里", "页边点亮书签，目录同步收录。", "Click to keep your place", "The marker and outline update together."),
    ("bookmark-03-outline.png", "书签，集中在目录里", "也可以按 ⌘D，继续标记下一段。", "Your bookmarks in the outline", "Press ⌘D to mark another passage."),
    ("bookmark-04-jump.png", "一点，回到原文", "点击目录书签，跳转并高亮段落。", "Return to the passage", "Click a bookmark to jump and highlight."),
    ("bookmark-05-all.png", "所有书签，随时找回", "⇧⌘D 打开书签列表，输入即可搜索。", "Find every bookmark", "⇧⌘D opens the searchable list."),
]
SPACES = [
    ("space-02-personal.png", "Personal", "粉与杏色，留给生活。", "Personal", "Rose and apricot, for everyday notes."),
    ("space-01-ideas.png", "灵感手记", "蓝、紫、绿，换一个空间，也换一种心情。", "Ideas", "Blue, violet and mint. A new space, a new mood."),
]


def escape(value):
    return html.escape(str(value), quote=True)


def text(x, y, content, size=20, fill="#253041", weight=400, extra=""):
    return f'<text x="{x}" y="{y}" font-size="{size}" fill="{fill}" font-weight="{weight}" {extra}>{escape(content)}</text>'


def timeline(count, seconds=3.2):
    """Layer each new frame over its predecessor; fade frame zero at the seam."""
    duration = count * seconds
    fade = .32
    pct = lambda t: f"{100 * t / duration:.4f}%"
    css = f'.frame{{opacity:0;animation-duration:{duration}s;animation-timing-function:linear;animation-iteration-count:infinite}}\n'
    for i in range(1, count):
        start, end = i * seconds, (i + 1) * seconds
        css += '@keyframes f%d {0%%,%s{opacity:0}' % (i, pct(start))
        if i == count - 1:
            css += '%s,100%%{opacity:1}}\n' % pct(start + fade)
        else:
            css += '%s,%s{opacity:1}%s,100%%{opacity:0}}\n' % (
                pct(start + fade), pct(end + fade - .01), pct(end + fade))
    css += f'@keyframes loop{{0%,{pct(duration-fade)}{{opacity:0}}100%{{opacity:1}}}}\n'
    css += '@media(prefers-reduced-motion:reduce){.frame{animation:none!important;opacity:0!important}}'
    return css, duration


def picture(data, x, y, w, h, clip):
    return f'<image x="{x}" y="{y}" width="{w}" height="{h}" clip-path="url(#{clip})" href="data:image/jpeg;base64,{data}"/>'


def material(colors, opacity, i, lang):
    """An optical comparison, separate from the unmodified app capture."""
    zh = lang == "zh-CN"
    stops = ''.join(f'<stop offset="{j/max(1,len(colors)-1):.3f}" stop-color="{c}"/>' for j, c in enumerate(colors))
    if len(colors) == 1:
        stops += f'<stop offset="1" stop-color="{colors[0]}"/>'
    art = '<rect width="204" height="238" fill="#e7ded6"/><rect x="-32" y="22" width="143" height="108" rx="28" fill="#d08da6"/><circle cx="172" cy="71" r="47" fill="#ebc568"/><path d="M-10 215Q72 95 214 196V258H-10Z" fill="#518b94"/>'
    art += ''.join(f'<rect x="26" y="{153+n*12}" width="{106-n*14}" height="3" rx="1.5" fill="#fff"/>' for n in range(3))
    return f'''<defs><linearGradient id="tint{i}" x2="0" y2="1">{stops}</linearGradient></defs>
<rect x="760" y="410" width="472" height="412" rx="24" fill="#eceff3"/>
{text(784,450,'透明叠色 × 背景模糊' if zh else 'Tint × background blur',22,weight=600)}
{text(784,479,'材质示意' if zh else 'Material illustration',14,fill='#637185')}
<g transform="translate(784 505)" clip-path="url(#sample)">{art}</g>
<g transform="translate(1004 505)" clip-path="url(#sample)"><g filter="url(#frost)">{art}</g><rect width="204" height="238" fill="url(#tint{i})" opacity="{opacity}"/></g>
{text(886,778,'清晰背景' if zh else 'Background',16,extra='text-anchor="middle"')}
{text(1106,778,'模糊后叠色' if zh else 'Blur + tint',16,extra='text-anchor="middle"')}
{text(760,862,'背景模糊来自 macOS 原生窗口材质。' if zh else 'Blur is provided by the native macOS material.',16,fill='#637185')}
{text(760,890,'强度控制颜色覆盖，形成不同的透明感。' if zh else 'Intensity changes how much color covers it.',16,fill='#637185')}
'''


def build(kind, lang, rows, images):
    zh = lang == 'zh-CN'
    theme = kind == 'themes'
    height = 1000 if theme else 932
    title = {'themes':('让颜色，成为空间的一部分','Make color part of your space'), 'bookmarks':('读到哪儿，回到哪儿','Pick up where you left off'), 'spaces':('换个空间，换种心情','A space for every part of life')}[kind][not zh]
    css, duration = timeline(len(rows), 3.2 if theme else 3.8)
    parts = [f'<svg xmlns="http://www.w3.org/2000/svg" width="1280" height="{height}" viewBox="0 0 1280 {height}" role="img" aria-labelledby="title desc">', f'<title id="title">{escape(title)}</title>', '<desc id="desc">'+escape('安装在本机的 ArcMD 0.0.7 客户端操作截图。画面裁切并淡入淡出；主题图右侧为单独标注的材质示意。' if zh else 'Screenshots of the installed ArcMD 0.0.7 client, cropped and cross-faded. The theme panel includes a separately labeled material illustration.')+'</desc>', f'<style>text{{font-family:{FONT}}}{css}</style>', '<defs><clipPath id="photo"><rect x="40" y="106" width="680" height="846" rx="24"/></clipPath><clipPath id="wide"><rect x="40" y="106" width="1200" height="726" rx="22"/></clipPath><clipPath id="sample"><rect width="204" height="238" rx="16"/></clipPath><filter id="frost" x="-30%" y="-30%" width="160%" height="160%"><feGaussianBlur stdDeviation="12"/></filter></defs>', f'<rect width="1280" height="{height}" rx="28" fill="#f7f8fa"/>', text(40,52,title,28,weight=600), text(1240,50,'ArcMD 0.0.7 · 本机客户端实拍' if zh else 'ArcMD 0.0.7 · Installed client',14,fill='#637185',extra='text-anchor="end"')]
    for i,row in enumerate(rows):
        heading, sub = (row[3:5] if zh else row[5:7]) if theme else (row[1:3] if zh else row[3:5])
        attrs = 'id="first"' if i == 0 else f'class="frame" style="animation-name:f{i}"'
        parts.append(f'<g {attrs}>')
        if theme:
            parts.append(picture(images[row[0]],40,106,680,846,'photo'))
            parts += ['<rect x="748" y="106" width="492" height="270" rx="24" fill="#fff"/>', text(776,147,f'0{i+1} / 08',14,fill='#68768a',weight=600),text(776,198,heading,27,weight=600),text(776,237,sub,18,fill='#637185')]
            for j,color in enumerate(row[1]):
                parts.append(f'<circle cx="{794+j*52}" cy="300" r="17" fill="{color}"/>')
            parts.append(material(row[1],row[2],i,lang))
            parts.append(text(40,978,'空间主题卡 · 实机局部' if zh else 'Space theme card · Captured detail',14,fill='#637185'))
        else:
            parts.append(picture(images[row[0]],40,106,1200,726,'wide'))
            parts += [text(40,877,heading,24,weight=600),text(40,909,sub,17,fill='#637185')]
        # The current stage remains visible even when motion is disabled.
        for n in range(len(rows)):
            parts.append(f'<rect x="{40+n*(1200/len(rows)):.2f}" y="78" width="{1200/len(rows)-7:.2f}" height="4" rx="2" fill="{"#6f77df" if n==i else "#dfe4ec"}"/>')
        parts.append('</g>')
    parts += ['<use href="#first" class="frame" style="animation-name:loop"/>','</svg>']
    return '\n'.join(parts)+'\n', duration


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--capture-dir',type=Path,required=True)
    parser.add_argument('--output-dir',type=Path,default=ROOT/'docs/images/readme')
    args=parser.parse_args()
    args.output_dir.mkdir(parents=True,exist_ok=True)
    provenance={'client':'/Applications/ArcMD.app','version':'0.0.7','capturedOn':'2026-11-12','captureMethod':'Native app screenshots via Computer Use. No dev build or headless UI rendering.','processing':'Crop and downsample; JPEG quality 85; cross-fade in SVG. Material comparison is separately labeled vector artwork.','frames':{},'animations':{}}
    for kind,rows,crop,size in [('themes',THEMES,'1200x1492+0+108','680x846!'),('bookmarks',BOOKMARKS,'2004x1212+460+108','1200x726!'),('spaces',SPACES,'2480x1500+0+100','1200x726!')]:
        images={}
        for row in rows:
            path=args.capture_dir/row[0]
            raw=path.read_bytes()
            # Fail closed on captures with different geometry instead of silently
            # cutting off a control or including another window.
            dimensions=subprocess.check_output(['magick','identify','-format','%wx%h',str(path)],text=True)
            if dimensions != '2480x1600':
                raise ValueError(f'{path}: expected 2480x1600, got {dimensions}')
            jpeg=subprocess.check_output(['magick',str(path),'-crop',crop,'+repage','-resize',size,'-strip','-sampling-factor','4:4:4','-quality','85','jpeg:-'])
            images[row[0]]=base64.b64encode(jpeg).decode('ascii')
            provenance['frames'][row[0]]={'sha256':hashlib.sha256(raw).hexdigest(),'crop':crop,'size':size,'embeddedJpegSha256':hashlib.sha256(jpeg).hexdigest()}
        for lang in ('zh-CN','en'):
            svg,duration=build(kind,lang,rows,images)
            path=args.output_dir/f'{kind}-{lang}.svg'
            path.write_text(svg)
            provenance['animations'][path.name]={'frames':len(rows),'durationSeconds':duration}
            print(f'{path.name}: {len(svg.encode())//1024} KiB, {len(rows)} frames, {duration:g}s')
    (args.output_dir/'live-capture.json').write_text(json.dumps(provenance,ensure_ascii=False,indent=2)+'\n')


if __name__=='__main__':
    main()
