#!/usr/bin/env python3
"""Build the README's self-contained vector animations in the hero's style.

Run from any directory: python3 scripts/readme_vector_art.py
The installed ArcMD client supplies the UI reference; no development client,
bitmap frames, JavaScript, external fonts, or SVG filters are used.
"""
from html import escape
from pathlib import Path
import math
import re

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "docs/images/readme"
FONT = '-apple-system,BlinkMacSystemFont,"SF Pro Text","Segoe UI","PingFang SC","Hiragino Sans GB","Microsoft YaHei","Noto Sans CJK SC",Helvetica,Arial,sans-serif'


def text(x, y, value, size=14, fill="#1f2329", weight=400, extra=""):
    return f'<text x="{x}" y="{y}" class="t" font-size="{size}" fill="{fill}" font-weight="{weight}" {extra}>{escape(value)}</text>'


def rect(x, y, w, h, fill, radius=0, extra=""):
    return f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="{radius}" fill="{fill}" {extra}/>'


def group(content, extra=""):
    return f'<g {extra}>{content}</g>'


def path(d, stroke="#5b4048", width=1.5, fill="none", extra=""):
    return f'<path d="{d}" fill="{fill}" stroke="{stroke}" stroke-width="{width}" stroke-linecap="round" stroke-linejoin="round" {extra}/>'


def mark(x, y, filled=False, extra=""):
    return group(path("M4 2h12a2 2 0 0 1 2 2v17l-8-4-8 4V4a2 2 0 0 1 2-2z", "#5874bb", 1.7, "#5874bb" if filled else "none"), f'transform="translate({x} {y})" {extra}')


def shadow(x, y, w, h, radius):
    return ''.join(rect(x-d, y+d/2, w+2*d, h+d, "#402733", radius+d, f'opacity="{opacity}"') for d, opacity in [(16, .018), (10, .025), (5, .035), (1, .05)])


def icon_tile(x, y, letter, color, size=16):
    return rect(x, y, size, size, color, round(size*.28, 1), 'fill-opacity=".16"') + text(x+size/2, y+size*.73, letter, size*.6, color, 600, 'text-anchor="middle"')


def keys(name, rows):
    return '@keyframes ' + name + '{' + ''.join(f'{at}{{{style}}}' for at, style in rows) + '}\n'


def animation(name, rows, selector=None):
    return keys(name, rows) + f'.{selector or name}' + '{animation-name:' + name + ';}\n'


def pos(x, y):
    return f'transform:translate({x}px,{y}px)'


DEFS = '''
<clipPath id="stage"><rect width="1280" height="820" rx="28"/></clipPath>
<clipPath id="win"><rect width="1240" height="760" rx="12"/></clipPath>
<clipPath id="card"><rect x="230" y="46" width="1002" height="706" rx="10"/></clipPath>
<clipPath id="reading"><rect x="498" y="78" width="716" height="674" rx="4"/></clipPath>
<linearGradient id="stageBg" x2="1" y2="1"><stop stop-color="#fff5f7"/><stop offset="1" stop-color="#f4f4fc"/></linearGradient>
<linearGradient id="shell" x1="0" y1="0" x2="0" y2="1"><stop class="a topColor" stop-color="#f8cddc"/><stop class="a midColor" offset=".5" stop-color="#f7d6cf"/><stop class="a bottomColor" offset="1" stop-color="#f1caee"/></linearGradient>
<linearGradient id="panel" x2=".4" y2="1"><stop stop-color="#faf6f7"/><stop offset="1" stop-color="#fffcfa"/></linearGradient>
<radialGradient id="pink"><stop stop-color="#ed78a9" stop-opacity=".56"/><stop offset=".55" stop-color="#ed78a9" stop-opacity=".2"/><stop offset="1" stop-color="#ed78a9" stop-opacity="0"/></radialGradient>
<radialGradient id="blue"><stop stop-color="#72c4df" stop-opacity=".7"/><stop offset=".5" stop-color="#72c4df" stop-opacity=".25"/><stop offset="1" stop-color="#72c4df" stop-opacity="0"/></radialGradient>
<radialGradient id="purple"><stop stop-color="#9c85db" stop-opacity=".64"/><stop offset=".5" stop-color="#9c85db" stop-opacity=".24"/><stop offset="1" stop-color="#9c85db" stop-opacity="0"/></radialGradient>
<pattern id="dots" width="4" height="4" patternUnits="userSpaceOnUse"><circle cx="2" cy="2" r=".52" fill="#64555e" opacity=".34"/></pattern>
<pattern id="grain" width="13" height="17" patternUnits="userSpaceOnUse"><circle cx="2" cy="3" r=".6"/><circle cx="9" cy="12" r=".65"/><circle cx="12" cy="6" r=".45"/><circle cx="5" cy="16" r=".5"/></pattern>
<g id="cursor"><path d="M0 0v23l5.7-5.7 5.1 10 4.5-2.3-5-9.5H19z" fill="#25242c" stroke="#fff" stroke-width="2" stroke-linejoin="round"/></g>
<g id="folder"><path d="M1 3.5a1.5 1.5 0 0 1 1.5-1.5h3.2l1.6 1.6h6.2A1.5 1.5 0 0 1 15 5.1V12.5A1.5 1.5 0 0 1 13.5 14h-11A1.5 1.5 0 0 1 1 12.5z" fill="#e8b1c8" stroke="#a14a71" stroke-width="1.25"/><path d="M1.6 7.2h13.6l-1.1 5.6a1.4 1.4 0 0 1-1.4 1.2H2.9a1.4 1.4 0 0 1-1.4-1.2z" fill="#f7dce7" stroke="#a14a71" stroke-width="1.25"/></g>
'''


def chrome(en):
    s = '<circle cx="21" cy="22" r="6" fill="#ff5f57"/><circle cx="41" cy="22" r="6" fill="#febc2e"/><circle cx="61" cy="22" r="6" fill="#28c840"/>'
    s += group(path("M202 15v14M252 22h-12m5-5-5 5 5 5M272 22h12m-5-5 5 5-5 5M315 18a6 6 0 1 0 1.5 5m-.2-8v4h-4M1147 16h10m-5 0v12M1186 15v14") + rect(197,15,16,14,"none",3,'stroke="#5b4048" stroke-width="1.5"') + rect(1176,15,16,14,"none",3,'stroke="#5b4048" stroke-width="1.5"'), 'opacity=".7" data-shell-stroke="true"')
    s += rect(336,7,740,31,"#fff",9,'fill-opacity=".5"')
    s += icon_tile(348,15,"R" if en else "读", "#ae3ec9", 15)
    s += text(372,27.5,"Personal / Reading notes.arcmd" if en else "Personal / 读书笔记.arcmd",13,"#493a46",500)
    s += text(1110,27.5,"142 words" if en else "142 字",12,"#6e5a60",400,'text-anchor="end" data-shell-ink="true"')
    s += '<g data-shell-ink="true" fill="#5b4048" opacity=".7"><circle cx="1210" cy="22" r="1.6"/><circle cx="1216" cy="22" r="1.6"/><circle cx="1222" cy="22" r="1.6"/></g>'
    for x, letter, color in [(10,"W" if en else "周","#7048e8"),(82.5,"P" if en else "项","#0c8599"),(155,"✦","#c74b83")]:
        s += rect(x,52,65,47,"#000",13,'fill-opacity=".045"') + icon_tile(x+22.5,65.5,letter,color,20)
    s += text(18,141,"✦",16,"#854764") + text(44,141,"Personal",13,"#6a4c59",600,'data-shell-ink="true"')
    s += text(205,141,"•••",13,"#6a4c59",600,'text-anchor="middle"')
    for y, title in [(190,"Journal" if en else "日记"),(231,"Ideas" if en else "灵感")]:
        s += f'<use href="#folder" x="18" y="{y-13}"/>' + text(44,y,title,14,"#493844",500,'data-shell-ink="true"')
    s += rect(8,256,214,36,"#fff",10,'fill-opacity=".72" stroke="#000" stroke-opacity=".04"')
    s += icon_tile(19,267,"R" if en else "读","#ae3ec9") + text(44,280,"Reading notes" if en else "读书笔记",14,"#493844")
    for y, title, letter in [(321,"Quotes" if en else "摘抄","Q" if en else "摘"),(362,"Book list" if en else "书单","B" if en else "书")]:
        s += icon_tile(19,y-13,letter,"#7048e8") + text(44,y,title,14,"#493844",400,'data-shell-ink="true"')
    s += path("M18 390h194", "#60404f",.7,extra='opacity=".15"')
    s += text(20,424,"+",18,"#785d69") + text(44,424,"New document" if en else "新建文档",13,"#785d69",400,'data-shell-ink="true" opacity=".65"')
    s += group(rect(17,724,16,13,"none",2.5,'stroke="#705664" stroke-width="1.4"')+path("M21 724v-4h8v4","#705664",1.4),'opacity=".65"')
    s += text(104,737,"✦",18,"#a34c76",600,'text-anchor="middle"')+text(143,737,"▧",17,"#886878",400,'opacity=".5" text-anchor="middle"')+text(205,737,"+",20,"#785d69",400,'text-anchor="middle"')
    return s


def captions(labels, classes):
    positions = [162, 430, 714, 986] if len(labels)==4 else [260, 570, 880]
    return ''.join(group(rect(x-30,760,22,22,"#b96e9c",6,'fill-opacity=".14"')+text(x-19,776,str(i+1),12,"#6b4161",600,'text-anchor="middle"')+text(x,776,label,14,"#513c4c",500),f'class="a {cls}"') for i,(x,label,cls) in enumerate(zip(positions,labels,classes)))


def scene(content, styles, desc, en, kind, captions_svg, duration):
    root = f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1280 820" width="1280" height="820" role="img" aria-label="{escape(desc, quote=True)}">\n'
    root += f'<style>\n.t{{font-family:{FONT}}}\n.a{{animation-duration:{duration}s;animation-iteration-count:infinite;animation-fill-mode:both;animation-timing-function:cubic-bezier(.65,0,.35,1)}}\n'+styles+'\n</style>\n<defs>'+DEFS+'</defs>\n'
    root += '<g clip-path="url(#stage)">'+rect(0,0,1280,820,'url(#stageBg)')
    root += '<circle cx="120" cy="130" r="360" fill="url(#pink)" opacity=".38"/><circle cx="1180" cy="670" r="410" fill="url(#purple)" opacity=".24"/><circle cx="100" cy="760" r="360" fill="url(#blue)" opacity=".3"/>'
    root += '<g transform="translate(70 34) scale(.919)">'+shadow(0,0,1240,760,12)
    root += '<g clip-path="url(#win)">'+rect(0,0,1240,760,"#f8f3f6")
    if kind == "themes":
        root += '<g class="a material" opacity=".26"><ellipse cx="70" cy="430" rx="220" ry="230" fill="url(#blue)"/><ellipse cx="235" cy="660" rx="200" ry="230" fill="url(#purple)"/><ellipse cx="80" cy="100" rx="250" ry="220" fill="url(#pink)"/></g>'
        root += rect(0,0,1240,760,"url(#shell)",extra='class="a tint"')+rect(0,0,1240,760,"url(#grain)",extra='opacity=".025"')
    else:
        root += rect(0,0,1240,760,"url(#shell)")
    root += chrome(en)+rect(230,46,1002,706,"#fff",10,'class="a paper" stroke="#000" stroke-opacity=".06"')
    root += content+'</g></g>'+captions_svg+'</g>\n</svg>\n'
    return root


def themes(en):
    css = ''
    colors = [
        ("0%,5%", ("#f8cddc",)*3), ("12%", ("#f5d5b6",)*3), ("20%,26%", ("#b9e2e8",)*3),
        ("29%,32%", ("#b9e2e8","#c0d8ec","#c6ceea")), ("35%,39%", ("#b9e2e8","#c6ceea","#c2e6ce")),
        ("45%", ("#d9cdf1","#f0c9dc","#c4d6ee")), ("52%,70%", ("#c4dfe9",)*3),
        ("77%,86%", ("#2e3040",)*3), ("95%,100%", ("#f8cddc",)*3)]
    for i,name in enumerate(["topColor","midColor","bottomColor"]):
        css += animation(name,[(at,'stop-color:'+c[i]) for at,c in colors])
    css += animation("tint",[("0%,52%","opacity:1"),("60%,65%","opacity:.24"),("71%,100%","opacity:1")])
    css += animation("material",[("0%,52%","opacity:.26;transform:scale(1)"),("60%,65%","opacity:1;transform:scale(1.06)"),("71%,100%","opacity:.26;transform:scale(1)")])
    css += animation("paper",[("0%,71%","fill:#fff"),("77%,86%","fill:#1f2023"),("95%,100%","fill:#fff")])
    css += animation("pageInk",[("0%,71%","fill:#1f2329"),("77%,86%","fill:#e3e4e7"),("95%,100%","fill:#1f2329")])
    css += '[data-shell-ink]{animation:pageInk 22s cubic-bezier(.65,0,.35,1) infinite both}\n'
    css += keys("chromeStroke",[("0%,71%","stroke:#5b4048"),("77%,86%","stroke:#d0c7d7"),("95%,100%","stroke:#5b4048")])
    css += '[data-shell-stroke] path,[data-shell-stroke] rect{animation:chromeStroke 22s cubic-bezier(.65,0,.35,1) infinite both}\n'
    # Each color handle orbits the pad as it does in the installed client.
    locations=[("0%,5%",108,-29),("12%",96,55),("20%,39%",-109,-19),("45%",19,-109),("52%,86%",-104,-38),("95%,100%",108,-29)]
    css += animation("knob",[(at,pos(x,y)) for at,x,y in locations])
    for n,angle in [(2,40),(3,-40)]:
        a=math.radians(angle)
        css += animation(f"orbit{n}",[(at,pos(round(x*math.cos(a)-y*math.sin(a),2),round(x*math.sin(a)+y*math.cos(a),2))) for at,x,y in locations])
    css += animation("extraColor2",[("0%,39%","fill:#829bd1"),("45%","fill:#d98fbe"),("52%,100%","fill:#829bd1")])
    css += animation("extraColor3",[("0%,39%","fill:#7bc798"),("45%","fill:#85a9d9"),("52%,100%","fill:#7bc798")])
    css += animation("knobColor",[("0%,5%","fill:#ed91b2"),("12%","fill:#eeaf77"),("20%,39%","fill:#75c8d1"),("45%","fill:#ad8ad8"),("52%,86%","fill:#86b9d6"),("95%,100%","fill:#ed91b2")])
    css += animation("extra2",[("0%,26%","opacity:0;transform:scale(.5)"),("29%,48%","opacity:1;transform:scale(1)"),("52%,100%","opacity:0;transform:scale(.5)")])
    css += animation("extra3",[("0%,32%","opacity:0;transform:scale(.5)"),("35%,48%","opacity:1;transform:scale(1)"),("50%,100%","opacity:0;transform:scale(.5)")])
    css += animation("slider",[("0%,53%",pos(198,0)),("60%,65%",pos(42,0)),("71%,100%",pos(198,0))])
    css += animation("mode",[("0%,71%",pos(0,0)),("77%,86%",pos(42,0)),("95%,100%",pos(0,0))])
    pointer=[("0%,5%",542,278),("12%",530,362),("20%,24%",325,288),("26%,35%",440,438),("39%",325,288),("45%",453,198),("48%,51%",404,438),("54%",480,548),("60%,65%",324,548),("70%",480,548),("73%,83%",468,155),("89%",426,155),("95%,100%",542,278)]
    css += animation("pointer",[(at,pos(x,y)) for at,x,y in pointer])
    for name,start,end in [("cap1",0,25),("cap2",25,52),("cap3",52,71),("cap4",71,94)]:
        if start==0:
            rows=[("0%,24%","opacity:1"),("29%,92%","opacity:.38"),("97%,100%","opacity:1")]
        else:
            rows=[(f"0%,{start-2}%","opacity:.38"),(f"{start+2}%,{end-2}%","opacity:1"),(f"{end+2}%,100%","opacity:.38")]
        css += animation(name, rows)
    css += '.extra2,.extra3{transform-box:fill-box;transform-origin:center}\n@media(prefers-reduced-motion:reduce){.a,[data-shell-ink],[data-shell-stroke] path,[data-shell-stroke] rect{animation:none!important}.pointer{display:none}.extra2,.extra3{opacity:1}.tint{opacity:1}.material{opacity:.26}.knob{transform:translate(108px,-29px)}.orbit2{transform:translate(101px,47px)}.orbit3{transform:translate(64px,-91px)}.slider{transform:translate(198px,0)}}'
    s = '<g clip-path="url(#card)">'
    values = [
        (146,"Leave a little space" if en else "留一点空白",32,600),
        (196,"A quiet page for the things on your mind." if en else "打开一页，记下此刻的想法。",16,400),
        (222,"Let the words find their own shape." if en else "让文字慢慢成为自己的形状。",16,400),
        (295,"Make it yours" if en else "为每个空间，留一种心情",23,600),
        (333,"A little color for a new idea." if en else "一点颜色，安放新的灵感。",16,400),
        (359,"A softer light for the end of the day." if en else "柔和的光，陪你慢慢写下去。",16,400),
        (444,"Today, at your own pace" if en else "今天，按自己的节奏",23,600),
        (485,"Read a few pages" if en else "读完几页喜欢的书",16,400),
        (523,"Write down a thought worth keeping" if en else "记下一个想留住的念头",16,400),
        (561,"Leave some room for tomorrow" if en else "给明天留一点空白",16,400),
        (670,"Your words. Your space." if en else "文字有自己的形状，空间也有。",16,400)]
    for y,label,size,weight in values:
        s += text(652,y,label,size,"#1f2329",weight,'data-page-ink="true"')
    css += '\n[data-page-ink]{animation:pageInk 22s cubic-bezier(.65,0,.35,1) infinite both}@media(prefers-reduced-motion:reduce){[data-page-ink]{animation:none!important}}'
    for y in [479,517,555]:
        s += '<circle cx="636" cy="'+str(y)+'" r="3" fill="#8295c2"/>'
    s += '</g>'
    s += shadow(246,118,355,468,22)+rect(246,118,355,468,"url(#panel)",22,'stroke="#6c5260" stroke-opacity=".13" stroke-width=".7"')
    s += rect(254,127,340,340,"#ece8e9",12,'fill-opacity=".68"')+rect(254,127,340,340,"url(#dots)",12)
    s += rect(359,139,132,32,"#6b5b65",10,'fill-opacity=".08"')
    s += rect(404,142,40,26,"#fff",7,'class="a mode" stroke="#6b5b65" stroke-opacity=".08"')
    s += path("M377 149l2-5 2 5 5 2-5 2-2 5-2-5-5-2zM386 144v-4m-2 2h4", "#786876",1.2)
    s += '<circle cx="424" cy="155" r="4.2" fill="none" stroke="#574653" stroke-width="1.4"/>'+path("M424 146v-2m0 22v-2m9-9h2m-22 0h2m3-6-2-2m14 14-2-2m0-10 2-2m-14 14 2-2","#574653",1.4)
    s += path("M470 148a7 7 0 1 0 3 10 6 6 0 0 1-3-10z","#786876",1.5)
    s += '<g transform="translate(424 297)"><g class="a knob"><circle r="52" fill="url(#pink)" opacity=".22"/><circle cy="3" r="24" fill="#6b4056" opacity=".12"/><circle r="22" fill="#ed91b2" stroke="#fff" stroke-width="3.5" class="a knobColor"/></g></g>'
    for n,color in [(2,"#829bd1"),(3,"#7bc798")]:
        s += f'<g transform="translate(424 297)"><g class="a orbit{n}"><g class="a extra{n}"><circle cy="2" r="15" fill="#6b4056" opacity=".12"/><circle r="13.5" fill="{color}" stroke="#fff" stroke-width="2.7" class="a extraColor{n}"/></g></g></g>'
    s += path("M399 438h10M431 438h10m-5-5v10", "#665660",1.7)
    s += path("M284 490l-4 5 4 5M567 490l4 5-4 5","#8a7a85",1.4)
    for i,color in enumerate(["#eee7e3","#e997ba","#a879a0","#ef677e","#fa9568","#f0ce62","#62d6a0","#74b6d0","#777393"]):
        s += f'<circle cx="{304+i*26}" cy="495" r="9.5" fill="{color}" stroke="#584450" stroke-opacity=".16"/>'
    s += rect(265,542,250,12,"#65575e",6,'fill-opacity=".09"')
    wave='M265 548'+''.join(f' C{x+7} 533 {x+14} 533 {x+21} 548 S{x+35} 563 {x+42} 548' for x in range(265,517,42))
    s += path(wave,"#66515e",2.8,extra='opacity=".8"')
    s += '<g transform="translate(265 548)"><g class="a slider">'+rect(-5,-15,10,30,"#fff",5,'stroke="#d9d4d8"')+'</g></g>'
    s += '<circle cx="552" cy="548" r="16" fill="#d0c8cc"/><circle cx="552" cy="548" r="16" fill="url(#grain)" opacity=".35"/>'
    s += path("M551 546l-6-8","#66515e",1.8)
    for i in range(18):
        a=i*math.tau/18
        s += f'<circle cx="{552+23*math.cos(a):.2f}" cy="{548+23*math.sin(a):.2f}" r="1.2" fill="#6e5d68" opacity=".65"/>'
    s += '<g class="a pointer"><use href="#cursor"/></g>'
    labels = ["Choose a color","Blend up to three","Soft, translucent glass","Light to dark"] if en else ["拖动，换一种颜色","一到三色，柔和渐变","通透与柔焦","浅色与深色"]
    desc = "ArcMD themes: a moving color handle, continuous color and multi-color gradient changes, translucent frosted material, and light-to-dark transitions." if en else "ArcMD 空间主题：拖动圆点连续换色，加入两色、三色渐变，调节强度呈现通透柔焦材质，再平滑切换浅色与深色。"
    return scene(s,css,desc,en,"themes",captions(labels,["cap1","cap2","cap3","cap4"]),22)


def bookmarks(en):
    css = animation("pointer",[("0%,3%",pos(853,302)),("8%,12%",pos(1190,295)),("17%",pos(934,494)),("22%,27%",pos(1190,493)),("34%,40%",pos(361,373)),("48%,52%",pos(765,476)),("61%,66%",pos(548,280)),("74%",pos(1000,448)),("79%,82%",pos(1190,493)),("87%,90%",pos(1190,295)),("97%,100%",pos(853,302))])
    css += animation("hover1",[("0%,5%","opacity:0"),("8%,90%","opacity:1"),("94%,100%","opacity:0")])
    css += animation("hover2",[("0%,18%","opacity:0"),("21%,82%","opacity:1"),("85%,100%","opacity:0")])
    css += animation("marked1",[("0%,11%","opacity:0;transform:scale(.75)"),("14%","opacity:1;transform:scale(1.15)"),("17%,87%","opacity:1;transform:scale(1)"),("90%,100%","opacity:0;transform:scale(.75)")])
    css += animation("marked2",[("0%,25%","opacity:0;transform:scale(.75)"),("28%","opacity:1;transform:scale(1.15)"),("31%,79%","opacity:1;transform:scale(1)"),("82%,100%","opacity:0;transform:scale(.75)")])
    css += animation("row1",[("0%,12%","opacity:0;transform:translateY(8px)"),("17%,87%","opacity:1;transform:none"),("92%,100%","opacity:0;transform:translateY(8px)")])
    css += animation("row2",[("0%,26%","opacity:0;transform:translateY(8px)"),("31%,79%","opacity:1;transform:none"),("84%,100%","opacity:0;transform:translateY(8px)")])
    css += animation("scrollPage",[("0%,38%",pos(0,0)),("44%,68%",pos(0,-200)),("74%,100%",pos(0,0))])
    css += animation("jumpGlow",[("0%,40%","opacity:0"),("44%,47%","opacity:1"),("52%,100%","opacity:0")])
    css += animation("outlineActive",[("0%,35%","opacity:0"),("39%,68%","opacity:1"),("73%,100%","opacity:0")])
    css += animation("palette",[("0%,53%","opacity:0;transform:translateY(8px) scale(.985)"),("57%,67%","opacity:1;transform:none"),("71%,100%","opacity:0;transform:translateY(8px) scale(.985)")])
    css += animation("dim",[("0%,53%","opacity:0"),("57%,67%","opacity:.16"),("71%,100%","opacity:0")])
    css += animation("shortcut",[("0%,49%","opacity:0;transform:translateY(6px)"),("52%,57%","opacity:1;transform:none"),("61%,100%","opacity:0;transform:translateY(6px)")])
    for name,start,end in [("cap1",0,17),("cap2",17,34),("cap3",34,52),("cap4",52,73)]:
        rows=[("0%,14%","opacity:1"),("19%,93%","opacity:.38"),("98%,100%","opacity:1")] if start==0 else [(f"0%,{start-2}%","opacity:.38"),(f"{start+2}%,{end-2}%","opacity:1"),(f"{end+2}%,100%","opacity:.38")]
        css += animation(name,rows)
    css += '.marked1,.marked2{transform-box:fill-box;transform-origin:center}.palette{transform-origin:620px 260px}.palette,.dim,.shortcut,.jumpGlow,.outlineActive{opacity:0}\n@media(prefers-reduced-motion:reduce){.a{animation:none!important}.pointer,.palette,.dim,.shortcut{display:none}.marked1,.marked2,.hover1,.hover2,.row1,.row2{opacity:1;transform:none}}'
    s = text(256,78,"«",16,"#8f959e")
    outline=[("Reading notes" if en else "读书笔记",114,0),("A quieter day" if en else "让日子慢一点",148,12),("A thought to keep" if en else "留住一个念头",181,12)]
    for title,y,dx in outline:
        s += text(256+dx,y,title,13,"#64728a" if dx else "#336df4",500 if dx==0 else 400)
    s += group(path("M256 278h207","#d9dce2",.8)+text(256,306,"BOOKMARKS" if en else "书签",11,"#9197a1",500), 'class="a row1"')
    s += group(mark(259,324)+text(287,340,"Slow down" if en else "慢下来，认真生活",13,"#63739a"),'class="a row1"')
    s += group(rect(250,354,216,32,"#e9eef8",7,'class="a outlineActive"')+mark(259,360)+text(287,376,"Keep a thought" if en else "记下值得留住的念头",13,"#63739a"),'class="a row2"')
    s += '<g clip-path="url(#reading)"><g class="a scrollPage">'
    s += text(520,138,"Reading notes" if en else "读书笔记",34,"#1f2329",600)
    s += text(520,185,"A page for the lines I want to come back to." if en else "读到喜欢的句子，就在这里留一个记号。",16,"#646a73")
    s += text(520,255,"A quieter day" if en else "让日子慢一点",24,"#1f2329",600)
    s += text(520,300,"Slow down. Notice the little things." if en else "慢下来，认真生活。留心那些细小、安静的美好。",16)
    s += text(520,326,"Some ideas deserve more than a passing glance." if en else "有些想法，值得停下来多看一眼。",16)
    s += rect(520,355,2,27,"#c4c7ce")+text(536,374,"Read a little. Think a little. Leave a little space." if en else "读几页书，想一件事，给今天留一点空白。",15,"#737982")
    s += text(520,453,"A thought to keep" if en else "留住一个念头",24,"#1f2329",600)
    s += rect(509,475,672,66,"#eaf0fc",8,'class="a jumpGlow"')
    s += text(520,498,"Write down the thought you want to keep." if en else "记下值得留住的念头，下一次可以从这里继续。",16)
    s += text(520,524,"You can always find your way back to this line." if en else "不必记住它在哪一页，书签会记得。",16)
    s += text(520,608,"On the next page" if en else "在下一页，继续",24,"#1f2329",600)
    for i,title in enumerate(["Read a chapter" if en else "读完一章喜欢的书","Save a line that surprised you" if en else "留下一句让你意外的话","Return when you need it" if en else "需要时，再回到这里"]):
        s += '<circle cx="528" cy="'+str(645+36*i)+'" r="3" fill="#336df4"/>'+text(546,651+36*i,title,16)
    for n,y in [(1,284),(2,482)]:
        s += group(mark(1181,y),f'class="a hover{n}"')
        s += group(mark(1181,y,True),f'class="a marked{n}"')
    s += '</g></g>'
    s += group(rect(722,646,166,44,"#f4f2f7",12,'stroke="#d5cedb" stroke-opacity=".6"')+text(805,675,"⇧ ⌘ D",20,"#61536e",500,'text-anchor="middle"'),'class="a shortcut"')
    s += rect(0,0,1240,760,"#211826",extra='class="a dim"')
    modal=shadow(320,154,600,315,16)+rect(320,154,600,315,"#fff",16,'stroke="#d6ced8" stroke-width=".7"')
    modal += path("M347 176a6 6 0 1 0 0 12 6 6 0 0 0 0-12m4 11 5 5","#9c91a3",1.6)
    modal += text(370,190,"All bookmarks" if en else "全部书签",16,"#716778")
    modal += path("M320 212h600","#eee9f0",1)
    modal += rect(330,226,580,62,"#f0edf6",9)
    for i,(title,detail) in enumerate([
        ("Slow down" if en else "慢下来，认真生活","Personal / Reading notes" if en else "Personal / 读书笔记"),
        ("Keep a thought" if en else "记下值得留住的念头","Personal / Reading notes" if en else "Personal / 读书笔记"),
        ("Next week's plan" if en else "下周的计划","Work / Weekly notes" if en else "工作 / 周报")]):
        y=250+i*68
        modal += mark(347,y-8,True)+text(383,y+1,title,14,"#3d3448",500)+text(383,y+22,detail,12,"#8b8192")
    modal += text(344,449,"↑↓ to choose" if en else "↑↓ 选择",11,"#a59aaa")+text(891,449,"↵ Open" if en else "↵ 打开",11,"#a59aaa",400,'text-anchor="end"')
    s += group(modal,'class="a palette"')+'<g class="a pointer"><use href="#cursor"/></g>'
    labels=["Hover a paragraph","Click to bookmark","Jump from the outline","⇧⌘D · All bookmarks"] if en else ["悬停，出现书签","点击，留下记号","从目录回到原文","⇧⌘D · 全部书签"]
    desc="ArcMD bookmarks: the pointer moves to a paragraph, adds two bookmarks, jumps smoothly from the outline, and opens all bookmarks with Shift Command D." if en else "ArcMD 书签：指针移到段落边，点击添加两枚书签，目录条目随之出现；点击目录平滑回到原文，按 Shift Command D 打开全部书签。"
    return scene(s,css,desc,en,"bookmarks",captions(labels,["cap1","cap2","cap3","cap4"]),20)


def main():
    OUT.mkdir(parents=True,exist_ok=True)
    for language,en in [("zh-CN",False),("en",True)]:
        for name,build in [("themes",themes),("bookmarks",bookmarks)]:
            svg=build(en)
            # Keep generated files reviewable without changing XML text nodes.
            svg=re.sub(r'><(?=g\b|/g>|text\b|rect\b)', '>\n<', svg)
            (OUT/f'{name}-{language}.svg').write_text(svg)
            print(f'{name}-{language}.svg: {len(svg.encode()):,} bytes')


if __name__ == "__main__":
    main()
