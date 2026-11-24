# Changelog

## 0.3.0

本版本正式推出 **纯 Go 原生两栖多维数据库引擎（`.db.md`）与 9 种高帧交互视图**，将 Notion / 飞书多维表格风格的结构化数据管理完整融入本地纯文本 Markdown 工作流——每条记录都是标准 `.md` 文件（YAML Frontmatter 双向同步属性），数据库文件自带可离线阅读的 GFM Markdown 表格快照；同时引入 **统一新建向导弹窗**、**文档内嵌数据库 Linked View 两栖块**、**侧边预览 / 居中弹窗 / 整页打开三模式**，以及对齐编辑器手感的 **120 Hz 弹簧微动效与悬浮拖拽落位引擎**。

### 新功能与桌面体验

- **两栖多维数据库内核（`internal/database`）**：支持文件夹记录源与内联行记录源，提供 18 种属性类型（标题、文本、数字/进度条/环形图、单选、多选、状态分组、日期与时间范围、复选框、负责人、URL、邮箱、电话、文件、自增编号 `UniqueID`、跨库双向 `Relation`、汇总 `Rollup`、`Formula 2.0` 表达式引擎与创建/修改时间）；支持多层筛选规则与相对日期过滤、多列排序、按属性分组、列统计聚合、条件行着色、记录模板、完整撤销/重做栈，以及 CSV / Notion ZIP 导出目录一键导入与 CSV / Markdown 导出。
- **9 种纯 Go 原生多视图与高帧动效引擎（`internal/dbview`）**：
  - **表格视图**：首列冻结、虚拟滚动、列宽拖拽调节、行列拖拽换位、行内与浮层属性编辑、条件着色与列统计底栏；支持 `KeepOffset` 保持排序滚动位置与弹簧换位过渡。
  - **看板视图**：泳道与状态/分类分组着色、卡片悬停微抬升、跨列拖拽修改状态、`Match / Fly` 跨列飞行过渡、列内键盘移动与内联新建卡片。
  - **日历视图**：支持「日 / 周 / 月 / 年（固定周与连续画布）/ 日程」五种模式、迷你月历与分类筛选侧栏、日程拖拽改期与边缘拉伸、单日详情弹窗。
  - **甘特时间轴视图**：支持日 / 周 / 月 / 季缩放、左右边缘拖拽调整起止排期、未排期抽屉与左侧属性表联动。
  - **列表、画廊、图表、动态流与表单视图**：画廊弹性多列封面卡片；原生柱状图 / 条形图 / 折线图 / 环形图；动态流快速发布；可视化表单构建与提交入库。
  - **120 Hz 物理微交互与无障碍**：基于 `ui.Overlay` 的悬浮拖拽预览卡（随指针速度轻微倾角与松手弹簧落位）、新建记录高亮脉冲、分段滑块 `Match` 平滑切换、统一玻璃风格弹出浮层，并全面遵守系统 `ReduceMotion` 偏好设置。
- **文档内嵌 Linked View 两栖块与三模式记录预览（`internal/editor/db_block.go`、`internal/app/database.go`）**：支持在任意 Markdown 文档中通过 `/` 菜单或命令栏嵌入已有数据库视图并调节高度，实时同步读写且自动写出 GFM 快照并支持矢量 PDF 打印；点击记录支持「侧边预览抽屉（可调宽） / 居中弹窗 / 整页打开」，在侧边栏中可直接展开数据库视图树并折叠同名记录文件夹。
- **统一新建向导与空间生态打通（`internal/app/newmodal.go`、`internal/app/search.go`、`internal/app/qa.go`）**：侧边栏与命令栏提供全新「新建」面板，一站式创建文档、数据库、白板、文件夹、套用模板或导入 CSV / Notion；数据库 Schema 与记录 Frontmatter 完整接入全局全文检索（`⌘K`）、空间问答 Agent（`⌘J`）与侧边栏重命名/移动联动。

### 关键修复与优化

- **修复排序后表格自动滚到底部的问题（`internal/dbview/table.go`、`list.go`、`timeline.go`）**：为表格、列表与时间轴虚拟列表开启 `KeepOffset` 与 `MoveAnimation`，避免因首行 key 移至表尾而带动视口跳转到底部。
- **修复整页打开数据库记录时侧边栏伴随文件夹无法收起的问题（`internal/app/sidebar.go`、`internal/app/opendocs.go`）**：打开数据库下的单条记录时不再强行永久展开已折叠的记录目录，保持「仅露出当前打开项」的 Arc FolderPeek 行为，点击折叠箭头或「关闭并保持置顶」均可正常收起。
- **补全中英文界面翻译与第三方归属说明（`internal/i18n/en.go`、`internal/database/query.go`、`third_party/README.md`）**：补齐数据库分组、统计标签、公式示例与工具栏缺失英文翻译，并同步更新 `third_party/README.md`。

### 下载说明

提供 macOS（Apple Silicon / Intel）、Windows x64 与 Linux（x64 `.deb` / `.tar.gz` / Arch Linux `.pkg.tar.zst`，ARM64 `.deb` / `.tar.gz`）原生安装包，可使用 `SHA256SUMS.txt` 校验完整性，已安装版本支持应用内签名自动更新。官方插件可在应用「设置 › 插件」按需从 [`Ciao1019/arcmd-plugins`](https://github.com/Ciao1019/arcmd-plugins) 下载或更新。

## 0.2.8

本版本新增 **HTML 与 EPUB 原生阅读、文档内嵌预览与空间检索**（[#64](https://github.com/Ciao1019/ArcMD/issues/64)）、**飞书风格有序列表编号值编辑**（[#62](https://github.com/Ciao1019/ArcMD/issues/62)）、**系统代理自动识别与插件市场失败重试**，以及 **Arch Linux 原生 `.pkg.tar.zst` 安装包与 CI 自动构建**（[#63](https://github.com/Ciao1019/ArcMD/pull/63)，感谢 `@Lyrecoul`）；同时彻底加固了 **输入 `/` 后粘贴文本与修饰键下的浮层行为**（[#64](https://github.com/Ciao1019/ArcMD/issues/64)）、**上下方向键跨块纵向导航** 与 **内嵌白板 / PDF / HTML / EPUB 块的选中删除体验**。

### 新功能与桌面体验

- **HTML 与 EPUB 原生阅读、文档内嵌与空间检索（`internal/bookdoc`、`internal/editor/bookview.go`、`internal/editor/book_block.go`、`internal/app/bookviewer.go`、`internal/app/htmlwebview.go`）**：支持在空间侧边栏、外部打开与拖放导入中直接阅读 `.epub`、`.html` 与 `.htm` 文件。EPUB 纯 Go 解析 EPUB 2（NCX）/ EPUB 3（Nav）目录、封面与内嵌图片，支持章节切换、目录侧栏、跨章与页内查找、页面宽度调节、DRM 加密与损坏提示，以及将当前章节另存为 Markdown 笔记；HTML 支持字符编码自动探测、标题大纲提取、沙箱化原生 WebView 忠实渲染（`Untrusted` 隔离并限制同目录本地资源访问）、阅读/源码视图切换、在默认浏览器中打开与另存为 Markdown。同时支持通过 `/` 菜单、命令栏或拖放将 HTML / EPUB 内嵌至 Markdown 文档就地浏览或弹窗放大，并完整接入空间全文检索、悬停预览卡片、矢量 PDF 导出与空间问答 Agent（`⌘J`）检索及引用跳转（[#64](https://github.com/Ciao1019/ArcMD/issues/64)）。
- **有序列表编号值编辑（`internal/document`、`internal/editor/listnum.go`、`internal/editor/menus.go`、`internal/editor/markdown.go`、`internal/editor/mindmap.go`）**：文档模型升级至 v10，支持 `1–999999` 起始编号，键入任意 `N. ` 自动按 `N` 起始；支持点击有序列表序号弹出编号设置浮层，或从块手柄菜单进入「编号设置 ›」执行「继续之前的编号」、「开始新列表」与「修改编号值」（含完整撤销/重做），并在 Markdown 导入导出（自动切换 `.` / `)` 分隔符无损保留分段起始号）、HTML 导入导出与思维导图中保持一致（[#62](https://github.com/Ciao1019/ArcMD/issues/62)）。
- **Arch Linux 原生打包与 CI 自动构建（`mygo.json`、`.github/workflows/build.yml`、`docs/building.md`）**：在 CI 的 `linux-amd64` 流程中通过固定镜像的离线 Arch Linux 容器自动构建 `.pkg.tar.zst` 安装包并生成 `SHA256SUMS` 校验，补全应用分类与运行时依赖（`hicolor-icon-theme`、`poppler-glib`）及本地 `makepkg` / `pacman -U` 安装文档（[#63](https://github.com/Ciao1019/ArcMD/pull/63)，感谢 `@Lyrecoul`）。
- **系统代理自动识别与插件市场重试（`internal/sysproxy`、`cmd/arcmd/main.go`、`internal/app/plugins.go`）**：启动时自动为标准 HTTP 客户端接入代理：优先遵循 `HTTP_PROXY` / `HTTPS_PROXY` 环境变量，未配置时回退读取操作系统代理设置（macOS 通过 `scutil --proxy` 支持 HTTP / HTTPS / SOCKS5，Windows 读取注册表 `Internet Settings`，带 5 秒缓存与本地地址绕过），让插件市场、空间问答 Agent、云端同步与软件更新自动走系统代理；同时将插件市场目录请求超时放宽至 30 秒，并在加载失败时提供「重试」按钮。

### 关键修复

- **彻底修复输入 `/` 后粘贴文本及修饰键下的浮层方向键问题（`internal/editor/tools.go`、`internal/editor/input.go`、`internal/editor/csvpaste.go`、`internal/editor/complete.go`、`internal/editor/wikilinks.go`）**：将斜杠菜单关闭状态改为按块 ID 记录，普通粘贴（含多行或以 `/` 开头的文本）、无格式粘贴及 CSV/TSV 粘贴建表后统一收起斜杠、`[[` 双链与自动补全菜单，不再拦截方向键；放行带修饰键的方向键组合，且删除 `/` 后可正常重新唤出菜单（[#64](https://github.com/Ciao1019/ArcMD/issues/64)）。
- **重写上下方向键纵向光标导航（`internal/editor/input.go`、`internal/editor/spatial.go`）**：基于空间索引实现 `moveVertical` 与 `nearestVertical`，按视觉行与块边界精确上下移动光标，修复跨标题、代码块、高亮块、表格列与图片/白板块时上下移动跳行或卡住的问题。
- **统一内嵌白板、PDF、HTML/EPUB 与异常图片块的选中与删除体验（`internal/editor/board_block.go`、`internal/editor/input.go`、`internal/editor/images.go`、`internal/editor/pdf_block.go`、`internal/editor/selection_scroll.go`、`internal/document/engine.go`）**：支持点击聚焦边框高亮、跨块拖选整块覆盖、`Backspace` / `Delete` 直接删除、`⌘C` / `⌘X` 复制剪切与撤销；加载失败的图片占位框支持点击聚焦和右键菜单（重新选择、显示文件、删除），白板悬停操作栏改为原生按钮控件以提升点击稳定性。
- **思维导图默认快捷键避让与工具栏角标对齐修复（`internal/app/keymap.go`、`internal/app/pdfviewer.go`、`internal/app/bookviewer.go`）**：将思维导图默认快捷键调整为 `⌃⌘M`（Windows / Linux `Ctrl+Alt+M`），并在 macOS 上将系统窗口快捷键 `⌘M` / `⌥⌘M` 设为保留组合；同时修复顶部工具栏中 PDF 页数与 EPUB 章节数角标垂直偏高的问题。

### 贡献者

- 特别感谢 **`@Lyrecoul`**（[#63](https://github.com/Ciao1019/ArcMD/pull/63)）对本版本的贡献！

### 下载说明

提供 macOS（Apple Silicon / Intel）、Windows x64 与 Linux（x64 `.deb` / `.tar.gz` / Arch Linux `.pkg.tar.zst`，ARM64 `.deb` / `.tar.gz`）原生安装包，可使用 `SHA256SUMS.txt` 校验完整性，已安装版本支持应用内签名自动更新。官方插件可在应用「设置 › 插件」按需从 [`Ciao1019/arcmd-plugins`](https://github.com/Ciao1019/arcmd-plugins) 下载或更新。

## 0.2.7

本版本紧急修复了 **macOS 空间内含损坏/异常 PDF 时启动后闪退及 PDF 解析器并发竞态问题**（[#57](https://github.com/Ciao1019/ArcMD/issues/57)、[#60](https://github.com/Ciao1019/ArcMD/pull/60)）与 **输入 `/` 后粘贴或无匹配结果时方向键失效的问题**（[#58](https://github.com/Ciao1019/ArcMD/pull/58)），同时新增 **飞书 / Notion 风格全宽页面模式与右上角页面宽度快速切换按钮**（[#41](https://github.com/Ciao1019/ArcMD/issues/41)、[#59](https://github.com/Ciao1019/ArcMD/pull/59)，感谢 `@tajiaoyezi`），并为 **Linux KDE Wayland 将窗口控制按钮融合进顶部工具栏**（[#56](https://github.com/Ciao1019/ArcMD/pull/56)，感谢 `@Lyrecoul`）。

### 新功能与桌面体验

- **全宽页面模式与右上角页面宽度切换（`internal/prefs`、`internal/editor/view.go`、`internal/app/pagewidth.go`）**：支持在文档右上角工具栏、专注模式顶栏、「⋯」更多菜单、「设置 › 外观」、命令栏与快捷键 `⌥⌘P`（Windows / Linux `Ctrl+Alt+P`）一键切换「窄（680）/ 标准（820）/ 较宽（1040）/ 全宽（铺满视口）」，全宽模式下自动保留左侧块手柄安全留白并避让大纲按钮，再次按下 `⌥⌘P` 自动恢复进入全宽前的栏宽（[#41](https://github.com/Ciao1019/ArcMD/issues/41)、[#59](https://github.com/Ciao1019/ArcMD/pull/59)，感谢 `@tajiaoyezi`）。
- **KDE Wayland 窗口控制按钮融合进工具栏（`internal/app/windowframe_linux.go`、`internal/app/windowbuttons.go`）**：在支持透明合成的 KDE Wayland 会话下启用客户端圆角边框，将最小化、最大化/还原与关闭按钮直接嵌入主窗口与独立文档窗口的顶部工具栏右侧（问答面板展开、分屏与窄窗下均保持可达，全屏自动隐藏，`Alt`/`F10` 唤出原生菜单），省去额外的系统标题栏，不影响 macOS、Windows 与其他 Linux 会话（[#56](https://github.com/Ciao1019/ArcMD/pull/56)，感谢 `@Lyrecoul`）。

### 关键修复

- **修复 macOS 空间内含损坏/异常 PDF 时启动闪退及解析器并发竞态（`internal/pdfdoc`）**：修复 `render_darwin.go` 打开失败或 0 页 PDF 时返回带类型的 `nil` 接口导致后台搜索索引及打开 PDF 时触发 `SIGSEGV` 空指针闪退的问题；为纯 Go `rawPDF` 解析缓存增加互斥锁消除并发读写 `map` 竞态，并加固内容流/CMap 孤立分隔符游标推进与交叉引用表边界防护（[#57](https://github.com/Ciao1019/ArcMD/issues/57)、[#60](https://github.com/Ciao1019/ArcMD/pull/60)）。
- **修复斜杠菜单与补全浮层拦截方向键的问题（`internal/editor/input.go`、`internal/editor/tools.go`）**：输入 `/` 后粘贴文本时立即关闭斜杠菜单；当 `/`、`[[`、`:`、`@` 过滤无匹配结果时放行上下左右与回车键，且在单列过滤列表中保留 `←` / `→` 移动查询光标（[#58](https://github.com/Ciao1019/ArcMD/pull/58)）。

### 贡献者

- 特别感谢 **`@tajiaoyezi`**（[#59](https://github.com/Ciao1019/ArcMD/pull/59)）与 **`@Lyrecoul`**（[#56](https://github.com/Ciao1019/ArcMD/pull/56)）对本版本的贡献！

### 下载说明

提供 macOS（Apple Silicon / Intel）、Windows x64 与 Linux（x64 / ARM64）原生安装包，可使用 `SHA256SUMS.txt` 校验完整性，已安装版本支持应用内签名自动更新。官方插件可在应用「设置 › 插件」按需从 [`Ciao1019/arcmd-plugins`](https://github.com/Ciao1019/arcmd-plugins) 下载或更新。

## 0.2.6

本版本新增 **原生零 CGO PDF 阅读器与文档内嵌 PDF 块、思维导图编辑与右键菜单、外部 `.md`/`.pdf` 拖拽导入**（[#36](https://github.com/Ciao1019/ArcMD/issues/36)、[#40](https://github.com/Ciao1019/ArcMD/issues/40)、[#44](https://github.com/Ciao1019/ArcMD/pull/44)），将官方扩展拆分为 **独立远程插件市场与图床自动上传**（[`Ciao1019/arcmd-plugins`](https://github.com/Ciao1019/arcmd-plugins)，[#45](https://github.com/Ciao1019/ArcMD/pull/45)），新增 **长代码块与长图片紧凑高度折叠与块内滚动**（[#43](https://github.com/Ciao1019/ArcMD/issues/43)、[#46](https://github.com/Ciao1019/ArcMD/pull/46)），并为 **独立 HTML 导出补齐代码语法高亮与 Mermaid 内联矢量 SVG**（[#51](https://github.com/Ciao1019/ArcMD/issues/51)、[#52](https://github.com/Ciao1019/ArcMD/pull/52)，感谢 `@NeverKnowsTomorrow`）；同时集中修复了侧栏误触、正文左侧 `+` 按钮裁切、白板画笔连续绘制与思维导图有序列表编号等多项体验问题（[#42](https://github.com/Ciao1019/ArcMD/issues/42)、[#47](https://github.com/Ciao1019/ArcMD/pull/47)、[#48](https://github.com/Ciao1019/ArcMD/pull/48)、[#49](https://github.com/Ciao1019/ArcMD/pull/49)、[#50](https://github.com/Ciao1019/ArcMD/pull/50)）。

### 新功能与生态

- **原生零 CGO PDF 阅读器、思维导图编辑与外部文件拖入（`internal/pdfdoc`、`internal/editor/pdfview.go`、`internal/editor/mindmap_actions.go`、`internal/app/filedrop.go`）**：基于系统原生能力（macOS PDFKit / Linux Poppler / Windows WinRT）实现零 CGO PDF 渲染与大纲/文本/全文检索集成；支持思维导图节点右键菜单、分支聚焦与结构化增删移动并导出 PNG/SVG；支持从系统访达/文件管理器拖拽 `.md` 与 `.pdf` 到窗口或侧边栏文件夹安全导入并支持一键撤销（[#36](https://github.com/Ciao1019/ArcMD/issues/36)、[#40](https://github.com/Ciao1019/ArcMD/issues/40)、[#44](https://github.com/Ciao1019/ArcMD/pull/44)）。
- **独立插件市场（`arcmd-plugins`）与图床自动上传插件（`internal/plugin`、`internal/app/imagehost.go`、`internal/app/remoteimage.go`）**：将内置插件剥离至独立仓库 [`Ciao1019/arcmd-plugins`](https://github.com/Ciao1019/arcmd-plugins) 并按需从 `catalog` Release 下载安装（SHA-256 校验），减小主程序安装包体积；新增官方「图床上传（`dev.arcmd.image-host`）」插件，支持粘贴/拖入图片自动上传 S3 兼容存储或自定义 HTTP 图床并改写外链、本地独立目录懒加载缓存、上传失败回退本地与单步撤销迁移（[#45](https://github.com/Ciao1019/ArcMD/pull/45)）。
- **长代码块与长图片紧凑高度及块内滚动（`internal/prefs`、`internal/editor/codewrap.go`、`internal/editor/images.go`、`internal/app/settings.go`）**：可在「设置 › 通用 › 写作」为长代码块和长图片分别配置紧凑最大高度与启用块内滚动，阅读时也可随时点击右上角展开/收起完整内容，且完全不污染原始 `.md` 文本或 PDF 打印输出（[#43](https://github.com/Ciao1019/ArcMD/issues/43)、[#46](https://github.com/Ciao1019/ArcMD/pull/46)）。
- **HTML 网页导出支持代码语法高亮与 Mermaid 内联矢量 SVG（`internal/mermaid/svg.go`、`internal/editor/htmlexport.go`、`internal/app/export.go`）**：导出 `.html` 网页沿用编辑器词法高亮与主题 Token，并将 Mermaid 流程图/时序图渲染为跟随系统明暗切换的 crisp 内联 SVG（解析失败时保留源码）；复制到剪贴板的语义 HTML 保持纯文本代码与 Mermaid 源码不变（[#51](https://github.com/Ciao1019/ArcMD/issues/51)、[#52](https://github.com/Ciao1019/ArcMD/pull/52)，感谢 `@NeverKnowsTomorrow`）。

### 体验优化与问题修复

- **移除侧边栏文件缝隙悬停「+ 新建文档」按钮（`internal/app/sidebar.go`）**：取消鼠标在左侧文档列表项之间停留时弹出的内联新建按钮，保留列表末尾稳定的新建文档入口与文件夹右键菜单，避免浏览文档时频繁误触干扰（[#47](https://github.com/Ciao1019/ArcMD/pull/47)）。
- **修复正文左侧 `+` 按钮裁切与点击拦截（`internal/editor/view.go`、`internal/editor/virtualize.go`、`internal/editor/blockselect.go`）**：将非打印布局左留白下限提升至 `80px`（窄分屏右侧自适应收敛），保证在打开思维导图、分屏、AI 问答、大纲或宽页面时，普通块、可折叠标题与表格左侧的 `+` 按钮完整可见，且不再被左空白拖拽框选拦截点击（[#48](https://github.com/Ciao1019/ArcMD/pull/48)）。
- **手绘白板自由画笔支持无框连续绘制（`internal/editor/board.go`、`internal/editor/board_view.go`）**：切换至自由画笔或落笔绘制期间不再选中元素并绘制控制框，且未锁定工具时画完一笔仍保持画笔状态以便连续手绘，切换回选择工具后点选再显示编辑框，对齐网页版 Excalidraw 交互（[#42](https://github.com/Ciao1019/ArcMD/issues/42)、[#49](https://github.com/Ciao1019/ArcMD/pull/49)）。
- **修复思维导图有序列表编号显示异常（`internal/editor/mindmap.go`、`internal/editor/view.go`）**：导图节点改为继承全文有序列表上下文计数器与原始缩进层级，修复所有条目都显示为 `1.` 的问题，并正确呈现嵌套 `a.` / `i.` 及两位数以上序号（[#50](https://github.com/Ciao1019/ArcMD/pull/50)）。

### 贡献者

- 特别感谢 **`@NeverKnowsTomorrow`**（[#52](https://github.com/Ciao1019/ArcMD/pull/52)）对本版本的贡献！

### 下载说明

提供 macOS（Apple Silicon / Intel）、Windows x64 与 Linux（x64 / ARM64）原生安装包，可使用 `SHA256SUMS.txt` 校验完整性，已安装版本支持应用内签名自动更新。官方插件随 [`Ciao1019/arcmd-plugins`](https://github.com/Ciao1019/arcmd-plugins)（`v0.1.1`）同步更新，可在应用「设置 › 插件」按需下载或更新。

## 0.2.5

This release adds **Sidebar Copy, Paste, and Duplicate** for documents, folders, and multi-selections ([#10](https://github.com/Ciao1019/ArcMD/issues/10), [#39](https://github.com/Ciao1019/ArcMD/pull/39)), **streaming Responses & Chat Completions protocol support** for Space Q&A ([#23](https://github.com/Ciao1019/ArcMD/pull/23)), **crisp math formula rendering, content-aware expanding tables, narrow-window sidebar hysteresis, and an optional Autosave toggle** ([#32](https://github.com/Ciao1019/ArcMD/issues/32)), plus targeted fixes for table row selection crashes ([#34](https://github.com/Ciao1019/ArcMD/issues/34), [#35](https://github.com/Ciao1019/ArcMD/pull/35)) and literal plain-text clipboard copying.

### Sidebar, Space Q&A, and Writing Workflow

- **Sidebar Copy, Paste, and Duplicate (`internal/vault/copy.go`, `internal/app/copy.go`, `internal/app/sidebar.go`)**: Right-click or use `⌘C` / `⌘V` (`Ctrl+C` / `Ctrl+V`) in the sidebar to copy, paste, or duplicate documents, nested folders, multi-selections, and favorite tiles within or across Spaces. Duplicate enters inline rename and opens the copy focused in the body; cross-folder and cross-Space pastes preserve or rebase attachments and covers safely with one-click Undo via Trash ([#10](https://github.com/Ciao1019/ArcMD/issues/10), [#39](https://github.com/Ciao1019/ArcMD/pull/39), thanks to `@tajiaoyezi`).
- **Streaming Responses and Chat Completions for Space Q&A (`internal/agent/responses.go`, `internal/app/qasettings.go`, `cmd/arcmd/ask.go`)**: Choose **Automatic**, **Chat Completions**, or **Responses** in Settings → AI Q&A or via `arcmd ask -protocol`. Automatic selects stateless streaming Responses for recognized reasoning models on OpenAI and OpenRouter while preserving pending tool-call results and encrypted reasoning replay ([#23](https://github.com/Ciao1019/ArcMD/pull/23), thanks to `@hyskr`).
- **Optional Autosave toggle with unsaved-changes guard (`internal/prefs/prefs.go`, `internal/app/unsaved.go`, `internal/app/settings.go`)**: Toggle background autosave in Settings → General → Writing or the command bar (`⌘K`). When disabled, edits remain in memory until explicitly saved (`⌘S`), and leaving or closing modified documents prompts **Save / Don't Save / Cancel** ([#32](https://github.com/Ciao1019/ArcMD/issues/32)).

### Math Rendering, Tables, and Clipboard Fixes

- **Crisp scaled math, wrapping equations, and content-aware tables (`internal/editor/math.go`, `internal/tex/layout.go`, `internal/editor/virtualize.go`, `internal/app/app.go`)**: Paint inline and display formulas directly at their target font size instead of raster-scaling pre-shaped glyphs, wrap long inline equations before shrinking, size table columns by natural content width and let wide formula tables expand past the standard prose column as the window widens, and add widening hysteresis so dragging a narrow window wider does not prematurely unfold the sidebar and squeeze the document ([#32](https://github.com/Ciao1019/ArcMD/issues/32)).
- **Fix table row selection panic (`internal/editor/table.go`)**: Guard column-index access in the table toolbar so selecting a row whose index exceeds the column count no longer triggers an out-of-range panic ([#34](https://github.com/Ciao1019/ArcMD/issues/34), [#35](https://github.com/Ciao1019/ArcMD/pull/35), thanks to `@Zzzt2003`).
- **Literal plain-text copy and cleaner Markdown escaping (`internal/editor/clipboard.go`, `internal/editor/markdown.go`)**: Copy literal characters without Markdown backslash escapes, backticks, or `<p align=...>` wrappers from table cells, inline code, and prose, and leave intraword underscores unescaped when exporting Markdown.

### Downloads

Packages are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`. Signed update archives and manifests support in-app updates.

## 0.2.4

This patch release fixes unbounded memory accumulation when switching repeatedly across Markdown documents, sidebar hover previews, and Spaces ([#33](https://github.com/Ciao1019/ArcMD/issues/33)).

### Memory and Resource Lifecycle Fixes

- **Reclaim closed documents and decoded image bitmaps on switch (`internal/app/app.go`, `internal/app/document.go`, `internal/app/opendocs.go`, `internal/app/split.go`, `internal/app/windows.go`, `internal/editor/images.go`)**: Retire closed `*document` instances and evict non-resident entries from `imgResolved` and `search.liveDocs` whenever switching notes, closing split panes or secondary windows, reloading, or unpinning tabs so closed editors and multi-megabyte RGBA `*ui.Bitmap` buffers are immediately eligible for garbage collection ([#33](https://github.com/Ciao1019/ArcMD/issues/33)).
- **Bounded async image decode concurrency and CPU pixel release (`internal/app/document.go`, `internal/app/view.go`)**: Cap concurrent background image decoders to 4 goroutines, abort queued or in-flight decodes as soon as a superseded document closes, and call `Bitmap.ReleasePixels()` after GPU texture upload on live windows so uncompressed RGBA/mipmap buffers do not remain duplicated in system RAM ([#33](https://github.com/Ciao1019/ArcMD/issues/33)).
- **Bounded sidebar thumbnail LRU cache and scaled preview decoding (`internal/app/preview.go`, `internal/app/document.go`, `internal/app/links.go`)**: Bound `preview.thumbs` to an 8-entry LRU cache, drop transient `open/...` clones when previews close, decode sidebar thumbnail images via `ui.DecodeBitmapScaled(data, 640)` instead of full-resolution bitmaps, and read `[[wiki-link]]` hover previews directly from the search index / text extractor without allocating thumbnail editors ([#33](https://github.com/Ciao1019/ArcMD/issues/33)).
- **Active Space session slot, whiteboard cache, and modal cleanup (`internal/app/app.go`, `internal/app/whiteboard.go`, `internal/app/versions.go`, `internal/app/gitmerge.go`)**: Clear the active Space's suspended `session.doc` reference upon returning to a Space, bound `wbCache` to 16 entries, and release retained document/diff state when the Version History, Git Changes, or 3-Way Merge modals close ([#33](https://github.com/Ciao1019/ArcMD/issues/33)).

### Downloads

Packages are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`. Signed update archives and manifests support in-app updates.

## 0.2.3

This release introduces a pure-Go WebAssembly plugin sandbox (`wazero`) with a bundled official **Cloud Sync** plugin (S3 SigV4 & WebDAV incremental bidirectional sync), an IDEA-style **3-Way Merge** and Git diff view, a navigable **Document Mind Map**, per-Space **Attachments Folders** with automatic relinking, native **`<kbd>` keycaps** and raw HTML / comment rendering, **Sidebar Multi-Select** batch file management, and targeted fixes across Mermaid sequence diagrams, TeX `\tag` numbering, Windows hardlinks, inline rename contrast, and macOS window lifecycle.

### Plugins, Cloud Sync, and 3-Way Merge

- **Pure-Go WASM plugin sandbox (`internal/plugin`, `internal/app/plugins.go`)**: Execute plugins safely inside a zero-cgo `wazero` WebAssembly sandbox with explicit permission confirmation (`document:read/write`, `space:read/write`, `network`, `secrets`, `ui:panel`), isolated local secret storage (`credentials/plugin-<id>-<key>`), declarative native settings forms, command-bar integration, and local `.zip` package installation.
- **Official Cloud Sync plugin (`plugins/cloud-sync`, `dev.arcmd.cloud-sync`)**: Built-in one-click installable WASM plugin providing three-way incremental bidirectional sync for Space documents and attachments over **S3** (AWS S3, Cloudflare R2, Alibaba Cloud OSS, Tencent Cloud COS, Backblaze B2, MinIO) and **WebDAV** (Jianguoyun, Nextcloud, Alist/OpenList), with 15-second post-save debounced sync and periodic background auto-sync.
- **Git-style Changes view and IDEA 3-Way Merge (`internal/merge`, `internal/app/gitmerge.go`)**: Inspect modified documents and sync conflicts as collapsible unified Git diff hunks with grapheme-level inline highlights, and resolve concurrent edits or cloud conflict copies in a three-pane **Yours / Result / Theirs** merge view with one-click connector arrows, auto-merge for non-conflicting hunks, and inline result fine-tuning.

### Document Mind Map, Raw HTML & `<kbd>`, and Space Management

- **Navigable document mind map (`internal/mindmap`, `internal/editor/mindmap.go`)**: Toggle a live, read-only mind map of any document (`⌥⌘M` / `Ctrl+Alt+M`, toolbar, or command bar) with single-sided and balanced two-sided tree layouts, smooth branch bezier curves, rich TeX/code/table node previews, viewport culling, zoom/pan controls, keyboard navigation, and click-to-jump editor synchronization ([#26](https://github.com/Ciao1019/ArcMD/pull/26), thanks to `@hyskr`).
- **Per-Space attachments folder and automatic relinking (`internal/app/attachmentdir.go`, `internal/space/space.go`)**: Configure a dedicated Space-wide attachments directory (`spaces.json` v10) from the Space menu or command bar, hide the attachments folder from the sidebar note tree, recalculate relative paths without duplicating files when moving notes within a Space, and automatically relink open and closed Markdown notes in the background when attachments are moved or renamed ([#27](https://github.com/Ciao1019/ArcMD/pull/27), [#25](https://github.com/Ciao1019/ArcMD/issues/25), thanks to `@Frieren924`).
- **Native `<kbd>` keycaps, raw HTML rendering, and HTML comments (`internal/document`, `internal/editor/mdstyle.go`, `internal/editor/markdown.go`, `internal/editor/livecompile.go`, `internal/editor/view.go`, `internal/editor/vectorpdf.go`)**: Render 3D rounded `<kbd>` keycaps across light/dark themes and vector PDF export (`Style.Kbd`, document format **9**), compile `<kbd>…</kbd>` and semantic inline HTML tags (`<u>`, `<ins>`, `<sup>`, `<sub>`, `<mark>`, `<br>`, `<q>`, `<var>`, `<dfn>`, `<cite>`, `<small>`, `<abbr>`, `<samp>`, `<tt>`, `<font color>`) live as you type with single-Backspace rollback, support `<div align="center|right">`, `<center>`, and `<hr>` blocks, and preserve both inline and standalone `<!-- ... -->` HTML comments losslessly while excluding hidden comments from HTML/PDF export, word counts, `#tags`, `[[wiki-links]]`, and mind maps ([#28](https://github.com/Ciao1019/ArcMD/pull/28), [#20](https://github.com/Ciao1019/ArcMD/issues/20), thanks to `@tajiaoyezi`).
- **Sidebar multi-select and batch file actions (`internal/app/multiselect.go`, `internal/app/treedrag.go`, `internal/app/crossspace.go`, `internal/app/palette.go`)**: Select multiple documents and folders with `⌘`-click (`Ctrl`-click on Windows/Linux) and `⇧`-click range selection, drag stacked selection cards with item-count pills to reorder, pin/unpin, move into folders, move across Spaces, add to Favorites, or open 2–4 selected notes in Split View, create a new folder containing the selected items, and undo batch moves or deletions in one click ([#21](https://github.com/Ciao1019/ArcMD/issues/21)).

### Mermaid, Math, Editor, and Platform Fixes

- **Full-width Mermaid sequence diagrams, `rect` shading, and vector zoom lightbox (`internal/mermaid/sequence.go`, `internal/editor/mermaid.go`)**: Lay out multi-phase `sequenceDiagram` blocks naturally across the text column without height-clamping shrinkage, render `rect rgb(...)` / `rgba(...)` phase background shading, and add a hover **Zoom** button (or double-click) opening a lossless vector zoom-and-pan diagram viewer ([#29](https://github.com/Ciao1019/ArcMD/issues/29)).
- **Display math `\tag` / `\tag*` equation numbering (`internal/tex`, `internal/editor/math.go`, `internal/editor/vectorpdf.go`)**: Parse and render right-aligned `\tag{...}` and unparenthesized `\tag*{...}` equation labels across single-line and multi-line display math blocks and vector PDF export ([#31](https://github.com/Ciao1019/ArcMD/issues/31)).
- **Preserve Windows and Unix hardlinks on save (`internal/fsutil`)**: Detect regular files with multiple hardlinks (`LinkCount > 1`) in `WriteFileAtomic` and fall back to in-place overwrite and truncation so saving hardlinked notes on Windows and Unix never breaks existing links ([#31](https://github.com/Ciao1019/ArcMD/issues/31)).
- **Readable inline rename contrast on selected items (`internal/app/sidebar.go`, `internal/app/favorites.go`)**: Clear selected-row light text overrides while renaming inline so document and folder names remain high-contrast and legible in both light and dark themes ([#30](https://github.com/Ciao1019/ArcMD/issues/30)).
- **Link boundary typing and macOS `⌘W` Dock lifecycle (`internal/document/engine.go`, `internal/editor/linkpopover.go`, `internal/app/app.go`)**: Avoid opening the link popover or extending link marks when clicking blank space after a link or typing at link start/end boundaries, and hide the main window on macOS `⌘W` / close button so ArcMD stays running in the Dock until `⌘Q` (restorable via Dock click or `⌘0`).

### Contributors

Special thanks to **`@hyskr`** ([#26](https://github.com/Ciao1019/ArcMD/pull/26)), **`@Frieren924`** ([#27](https://github.com/Ciao1019/ArcMD/pull/27)), and **`@tajiaoyezi`** ([#28](https://github.com/Ciao1019/ArcMD/pull/28)) for their merged pull requests in this release!

### Documentation

- Update `README.md`, `README.en.md`, `AGENTS.md`, `docs/architecture.md`, `docs/plugins.md`, and `tools/dev` for version `0.2.3`, WASM plugins and Cloud Sync, document mind maps, per-Space attachments folders, `<kbd>` / raw HTML support, and sidebar multi-selection.

### Data compatibility

Markdown (`.md`, `.markdown`, `.mdx`) remains the primary in-place format. Legacy `.arcmd` JSON advances to format **9** (adding optional `Style.Kbd` and `Block.Comments`, with automatic migration from formats **1–8**), and Space configuration (`spaces.json`) advances to format **10** (adding optional per-Space `attachmentsDir`, with automatic migration from earlier formats). Plugin configuration and state live under `plugins/`, while plugin secrets are stored separately in `credentials/` (`0600`).

### Downloads

Packages are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`. Signed update archives and manifests support in-app updates. The pure-Go WASM plugin sandbox (`wazero`), local `.zip` plugin installer, and bundled official Cloud Sync plugin (`dev.arcmd.cloud-sync`) are included; the remote plugin marketplace index and `process` runtime remain in development.

## 0.2.2

This release introduces pure-Go Excalidraw-compatible hand-drawn whiteboards across standalone files and Markdown documents, upgrades the MyGo UI runtime to `v0.10.0`, and resolves key editing bugs covering code block indentation, hierarchical `⌘A` / `Ctrl+A` selection, nested list indentation with single-Backspace tabs, and `Space` key viewport jumping.

### Native hand-drawn whiteboards

- **Pure-Go Excalidraw whiteboard engine (`internal/whiteboard`, `internal/editor/board*.go`, `internal/app/whiteboard.go`)**: Create standalone amphibious `.excalidraw.svg` (and `.excalidraw`) whiteboard documents (`⌥⌘B` / `Ctrl+Alt+B`, sidebar, or command bar), insert inline ```` ```excalidraw ```` blocks from `/`, or embed `![[*.excalidraw.svg]]` boards inside Markdown documents with double-click / modal editing and one-click extraction to standalone files.
- **Hand-drawn shapes, connectors, libraries, and Workspace integration**: Draw with 19 shape and connector tools (including rectangle, diamond, ellipse, triangle, parallelogram, hexagon, cylinder, cloud, star, callout, document, stadium, actor, freedraw, text, sticky note, frame, arrow, and line plus eraser and laser pointer), Rough-style strokes with hachure/cross-hatch/solid fills, magnetic arrow bindings with sharp/curved/elbowed routing, smart snap guides, Mermaid-to-whiteboard conversion, built-in and importable `.excalidrawlib` shape libraries (`whiteboard-library.excalidrawlib`), `[[wiki-link]]` badges and backlinks, Space Q&A indexing and element-level citation jumps, version history/restore, and pure-Go vector PDF and standalone HTML export.

### Editor and keyboard fixes

- **Code block visual tabs, soft-wrap indentation, paste recovery, and Enter/Tab behavior (`internal/editor/input.go`, `internal/editor/clipboard.go`, `internal/editor/codewrap.go`, `internal/editor/style.go`, `internal/editor/vectorpdf.go`, `internal/document/engine.go`)**: Render leading `\t` tabs visually in code blocks and vector PDFs, preserve visual leading indentation across soft-wrapped long lines, preserve and recover leading indentation when pasting HTML code snippets (`<pre>` / `white-space: pre`), auto-indent on `Enter` in code blocks while cleaning abandoned blank-line indentation, and support literal `\t` insertion plus multi-line `Tab` / `Shift+Tab` indent/outdent ([#19](https://github.com/Ciao1019/ArcMD/issues/19)).
- **Hierarchical `⌘A` / `Ctrl+A` Select All (`internal/editor/input.go`)**: Expand selection progressively on repeated `⌘A` / `Ctrl+A`—inline math body → current block or table cell → enclosing column or table → full document (plus all top-level blocks in block selection mode)—without jumping the viewport scroll position ([#18](https://github.com/Ciao1019/ArcMD/issues/18)).
- **Nested list indentation and single-Backspace tabs (`internal/editor/input.go`, `internal/document/engine.go`, `internal/editor/markdown.go`)**: Indent and outdent single or multi-item bullet, numbered, and task lists cleanly with `Tab` / `Shift+Tab`, step down indentation before unstyling on `Backspace` or empty `Enter`, preserve nested ordered-list levels across Markdown round-trips, and insert `\t` in prose so a single `Backspace` deletes the whole tab ([#17](https://github.com/Ciao1019/ArcMD/issues/17)).
- **Prevent `Space` paging while editing (`internal/editor/input.go`, `internal/editor/view.go`, `internal/editor/buffer.go`, `internal/editor/reading.go`)**: Consume `Space` and `Shift+Space` key events in the editable writing surface so MyGo does not scroll the page while typing or composing IME text, while keeping `Space` / `Shift+Space` viewport paging working cleanly in read-only mode without triggering the locked-page notice ([#16](https://github.com/Ciao1019/ArcMD/pull/16), [#7](https://github.com/Ciao1019/ArcMD/issues/7), thanks to `@Raspercival`).

### Runtime and performance

- **MyGo `v0.10.0` and baseline tracking (`go.mod`, `go.sum`, `internal/document/engine.go`, `internal/app/document.go`, `tools/perf/overlay.go`)**: Upgrade `github.com/egoist/mygo` from `v0.7.4` to `v0.10.0`—measuring **0.248 ms P50** typing and **0.447 ms P50** scrolling on the 1M-character mixed benchmark—and add leaf-scoped baseline run tracking (`OnBeforeLeafChange`) to avoid full-document dirty recomputation on single-leaf edits.

### Documentation

- Update `README.md`, `README.en.md`, `AGENTS.md`, `docs/building.md`, `tools/dev.sh`, `tools/perf/README.md`, and `.github/workflows/build.yml` for version `0.2.2`, hand-drawn whiteboards, and MyGo `v0.10.0`.

### Data compatibility

Document (`.md` and legacy `.arcmd` format **8**) and Space configuration (`spaces.json` format **9**) formats are unchanged from 0.2.1. Spaces may also contain optional Excalidraw-compatible `.excalidraw.svg` / `.excalidraw` whiteboard files, and the configuration directory may store user whiteboard library items in `whiteboard-library.excalidrawlib`.

### Downloads

Packages are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`. Signed update archives and manifests support in-app updates. The plugin marketplace and plugin execution runtimes remain unavailable.

## 0.2.1

This release upgrades PDF export to a pure-Go vector pipeline with hierarchical outlines and clickable links, adds native Mermaid diagram rendering, `\boldsymbol` bold math fonts, and Obsidian `![[...]]` image embed compatibility, speeds up large-document editing and command-bar discovery with Pinyin/multi-keyword search and persistent caching, introduces named version milestones with grapheme-level inline diffs, and resolves Windows shortcut and path-handling issues.

### Vector PDF export and native Mermaid diagrams

- **Pure-Go vector PDF export (`internal/pdf`, `internal/editor/vectorpdf.go`)**: Export selectable, searchable PDFs directly as scalable vector paths, rectangles, and embedded Type 3 glyph subsets from KaTeX math (regular, bold, italic, and bold-italic) and system fonts with `/ToUnicode` mappings—eliminating full-page raster JPEG streams while embedding opaque images cleanly. Exported PDFs include hierarchical `/Outlines` bookmarks generated from document headings and clickable `/Link` annotations for external URLs and internal `#heading` anchors.
- **Native Mermaid diagrams (`internal/mermaid`, `internal/editor/mermaid.go`)**: Render `flowchart` / `graph` (layered layout, subgraphs, 11 node shapes, solid/dashed/thick labeled edges) and `sequenceDiagram` (participants, actors, sync/async arrows, activations, notes, and `alt`/`opt`/`loop`/`par` fragments) directly in Go with light/dark theme tokens—no JavaScript or webview. Focusing a Mermaid block opens an editing card with live diagram preview above a monospace source editor (`↵↵` or `Tab` exits below); diagrams insert from `/` or the command bar and export into both standalone HTML and vector PDFs.

### Search, code blocks, math, and version history

- **Mandarin Pinyin, multi-keyword search, and persistent index cache (`internal/pinyin`, `internal/fuzzy`, `internal/search`)**: Match Chinese document titles and Space/folder hints by full Pinyin or initials (e.g. `zb` or `zhoubao`), combine whitespace-separated tokens across titles and full-text search, jump directly to matched fallback terms in the editor, and persist an atomic `0600` disk index snapshot (`search-cache.bin`) for instant startup search.
- **Stateful incremental syntax highlighting and code soft-wrap toggle (`internal/editor/highlight.go`, `internal/editor/codewrap.go`)**: Upgrade code blocks to a line-cached stateful lexer supporting 17 languages (Go, Rust, C/C++/Java, JS/TS, Python, Ruby, PHP, Shell, JSON/JSONC, YAML, TOML, SQL, HTML, CSS, Markdown, and Mermaid), live ```` ```lang ```` fence conversion on Enter, a **Settings › General** default wrap preference (`codeNoWrap`), and per-block wrap toggles with horizontal scrolling.
- **Native `\boldsymbol` math rendering (`internal/tex`, `internal/editor/math.go`)**: Parse and render `\boldsymbol{...}` across display math, inline math, bookmark previews, and vector PDF export using embedded unmodified `KaTeX_Main-Bold.ttf` and `KaTeX_Math-BoldItalic.ttf` faces (`9b2280d`), carrying scoped bold styling and glyph cache keys while preserving operator spacing, limits, and integral upper-hook clearance ([#4](https://github.com/Ciao1019/ArcMD/pull/4), thanks to `@Raspercival`).
- **Version milestones, grapheme-level inline diffs, and Fenwick virtualization (`internal/history`, `internal/app/versions.go`, `internal/editor/fenwick.go`)**: Save, rename, and filter protected named milestones in `.arcmd/history` (`labels.json`), highlight exact intra-line character and word edits with Unicode grapheme-aware LCS diffing, and index virtualized row heights with an `O(log N)` Fenwick tree for large documents.

### Obsidian compatibility and Windows fixes

- **Obsidian `![[...]]` image embeds and Space attachment lookup (`internal/editor/markdown.go`, `internal/wiki/wiki.go`, `internal/app/document.go`, `internal/app/relocate.go`)**: Import standalone and back-to-back Obsidian image embeds (`![[image.png]]`, `![[folder/image.png|360]]`, `![[image.png|360x240]]`, and `![[image.png|caption|width]]`) into native image blocks while preserving untouched `![[...]]` source lines verbatim on save, excluding `![[...]]` embeds from note wiki-links and `[[` completion, and resolving Space-root or bare attachment filenames across non-hidden Space directories while keeping External Space isolation strict ([#6](https://github.com/Ciao1019/ArcMD/issues/6)).
- **Windows shortcut defaults, menu labels, and path error handling (`internal/editor/keys.go`, `internal/app/keymap.go`, `internal/fsutil/fsutil.go`, `internal/vault/vault.go`, `internal/prefs/secrets.go`, `internal/app/document.go`)**: Bind `overview`, `prev-bookmark`, and `next-bookmark` to `Ctrl+Alt+S` / `Ctrl+Alt+Up/Down` outside macOS so they do not collapse into `Ctrl+S` or `Ctrl+Up/Down`, format non-macOS `Combo.Label()` values as `Ctrl+Shift+K`, distinguish regular-file path blockers from missing files under Windows `ERROR_PATH_NOT_FOUND`, and run `go test ./...` on `windows-2022` in CI ([#5](https://github.com/Ciao1019/ArcMD/issues/5)).

### Documentation

- Update `README.md`, `README.en.md`, `AGENTS.md`, and `third_party/README.md` for version 0.2.1, vector PDF export, native Mermaid diagrams, `\boldsymbol`, Pinyin/cached search, version milestones, Obsidian `![[...]]` image embeds, and Windows fixes.

### Data compatibility

Document (`.md` and legacy `.arcmd` format **8**) and Space configuration (`spaces.json` format **9**) formats are unchanged from 0.2.0. Preferences (`prefs.json` v1) add an optional `codeNoWrap` boolean; document history adds an optional `.arcmd/history/<doc>/labels.json` milestone map; and the configuration directory stores an optional regenerable `search-cache.bin` snapshot.

### Downloads

Packages are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`. Signed update archives and manifests support in-app updates. The plugin marketplace and plugin execution runtimes remain unavailable.

## 0.2.0

This release introduces **Space Q&A**—a built-in, read-only AI research agent that searches and reads a Space's notes directly without a vector database—alongside viewport-aware floating menus, smarter live Markdown and display-math input, and large-document virtualization.

### Space Q&A

- **Zero-index agentic search and reading**: Press `⌘J` (or click the toolbar Q&A icon) to open a per-Space side panel. Instead of building a vector index, the agent explores a live, line-numbered Markdown projection of the Space—including unsaved edits in open documents and registered External space files—using parallel read-only tools (`glob`, case-insensitive and regex `grep`, `read`, `outline`, and `links` for wiki-links, backlinks, and `#tags`) within a bounded 16-step budget, treating note content strictly as untrusted data rather than instructions.
- **Interactive inline citations and gliding previews**: Answers cite exact `[path#L12-L18]` ranges rendered as inline monogram source chips with `+N` multi-source badges. Hovering or focusing a chip highlights the supporting claim in the answer and floats a gliding preview card beside the panel with the document icon, section heading, line range, and recalled excerpt; expanding the stacked-monogram **Sources** footer syncs hover highlights bidirectionally with inline claims, and clicking any chip or source jumps straight to the cited block in the editor with a highlight glow.
- **Paced streaming, exploration timeline, and native polish**: Streamed Markdown answers (headings, lists, blockquotes, code blocks, tables, and inline marks) use adaptive balanced pacing and per-character fade-in, paired with a collapsible step-by-step search/read timeline, a `Jump to latest` pill while reading earlier passages, localized error states with one-click retry (plus automatic retries on transient 429/5xx/stream cutoffs before text starts), and a pointer-tracking ghost avatar.
- **Bring-your-own Chat Completions model and CLI runner**: Configure **Settings › AI Q&A** with presets for **OpenAI**, **DeepSeek** (with automatic `reasoning_content` replay across tool calls), **OpenRouter**, local **Ollama** (keyless `localhost` HTTP supported; remote endpoints require `https://`), or any custom Chat Completions service, including one-click `/models` fetching and a searchable model picker. API keys are stored atomically outside `prefs.json` and Space folders in `credentials/ai-api-key` (`0600`). Run `arcmd ask` in the terminal to inspect tool traces and verify citations from the command line.

### Fixes and editor improvements

- **Viewport-aware floating menus and submenus**: Flip block-handle menus, `/`, `[[`, and `:`/`@` completion menus, callout color/icon pickers, and code-language pickers above the anchor when bottom viewport space is tight, clamp height with inner scrolling when neither side fits full height, keep side submenus (`Indent and Alignment`, `Color`) inside horizontal and vertical window bounds, and keep `/`, completion, and command-bar selections scrolled into view during arrow-key navigation ([#2](https://github.com/Ciao1019/ArcMD/issues/2)).
- **Reverse live Markdown and predictive display math**: Prepending block prefixes (`#`–`###### `, `> `, `- `, `1. `, `- [ ] `) or opening inline delimiters (`**`, `*`, `~~`, `` ` ``) before existing text now compiles in place while preserving trailing content, with single-Backspace rollback to literal syntax. Typing `$$` in an empty paragraph immediately opens a display-math card (`Backspace` or `Esc` restores literal `$$`), single-line `$$ … $$` compiles live, and `$$ ` opens display math even when auto-pairing is off.
- **Large-document virtualization and O(1) dirty tracking**: Virtualize offscreen paragraph shaping on documents exceeding 800 leaves or 120,000 characters with pointer-free 128-bit run fingerprints and bounded LRU caches; replace full-document JSON baseline clones with streaming digests and leaf-level dirty tracking; keep single-leaf IME composition and inline formatting clone-free; extract plain text directly from Markdown when indexing search; defer backlink/tag footer recomputation while actively typing in long documents; and ignore atomic-save temp-dotfile overwrites in the directory watcher.

### Documentation

- Refresh `README.md`, `README.en.md`, `AGENTS.md`, `docs/architecture.md`, `docs/plugins.md`, and `third_party/` notices for Space Q&A and version 0.2.0.

### Data compatibility

Document (`.md` and legacy `.arcmd` format **8**) and Space configuration (`spaces.json` format **9**) formats are unchanged from 0.1.0. Preferences (`prefs.json` v1) add optional `ai` service fields (`provider`, `baseURL`, `model`), while the API key is stored separately in `credentials/ai-api-key`. Older releases ignore unknown preference fields.

### Downloads

Packages are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`. Signed update archives and manifests support in-app updates. The plugin marketplace and plugin execution runtimes remain unavailable.

## 0.1.0

This release makes standard `.md` Markdown the primary document format, adds a built-in External space for standalone files, and brings fluid motion and tactile micro-interactions across typing, math, tables, images, and the sidebar.

### Native Markdown and external files

- Save and autosave `.md`, `.markdown`, and `.mdx` documents directly in place without creating sidecar `.arcmd` copies. Unmodified top-level blocks preserve their original Markdown formatting, while rich attributes—colors, highlights, hand-drawn annotations, callouts, toggles, merged or colored tables, custom two-column widths, and page icons, covers, and word goals—round-trip losslessly through standard Markdown, semantic HTML, and YAML frontmatter.
- Open standalone files outside any Space in a built-in **External** space (`Open File…` or OS file associations) without scanning parent folders. Edits save in place, images resolve relative to each file's folder, and removing an entry drops only the reference. Legacy `.arcmd` documents and snapshots remain supported.

### Fluid writing and micro-interactions

- Glide the caret smoothly across characters, lines, and math capsules; scroll with interruptible exponential damping for typewriter mode, edge tracking, and search or footnote jumps; and animate block, fold, indent, and table row/column displacement without disturbing document coordinates.
- Compile inline `**bold**`, `*italic*`, `~~strike~~`, `` `code` ``, `--`, `...`, and `---` live as you type, with one-keystroke Backspace rollback to literal text. Auto-pair brackets, smart curly quotes, and inline `$…$` math with soft scope highlights and `Tab` jump-out badges.
- Preview inline `$…$` TeX in a floating source capsule below the line without shifting baselines, and edit display math in a unified preview-and-source card (`↵↵` or `Tab` to continue prose).
- Unfold `| A | B |` pipe headers into native tables on Enter, drag table rows and columns to reorder, resize two-column seams alongside table columns and split panes with `50% · 50%` magnetic snap pills, and toggle task checkboxes with spring motion.
- Preview dragged images as tilted Polaroid cards that spring upright on drop, size local images immediately from file headers without layout jumps, open double-clicked images in a damped hero lightbox, and save documents and history snapshots asynchronously off the UI thread.
- Refine sidebar drag-and-drop with floating cards and dashed insertion slots, inline hover `+ New Document` seams, instant zero-blocking command bar opening, directional back/forward transitions, and synchronized rich-text ⇄ Markdown preview cross-fades.

### Documentation

- Refresh the Chinese and English READMEs around a top-of-page fluid writing showcase and rebuild all localized vector animations from a unified `tools/readme-art` generator.

### Data compatibility

ArcMD 0.1.0 writes Space configuration format **9** (adding the built-in External space map) and continues to read and write legacy `.arcmd` document format **8** alongside native `.md` files. Older releases cannot open `spaces.json` v9; keep a copy of your configuration directory if you need to return to an earlier version.

### Downloads

Packages are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`. Signed update archives and manifests support in-app updates. The plugin marketplace and plugin execution runtimes remain unavailable.

## 0.0.11

This release adds hand-drawn annotations, more ways to write and organize documents, and broad improvements to the editor's algorithms for long documents.

### Writing and expression

- Mark text with 12 hand-drawn styles, including circles, waves, highlights, arrows, brackets, and boxes. Choose colors, edit existing marks, and watch strokes draw themselves in.
- Select multiple blocks with the keyboard or margin, then move, indent, duplicate, convert, or delete them together. Add toggle blocks to fold a section of notes.
- Insert emoji with shortcodes such as `:smile:` and use `@` for dates and document links. Optional automatic spacing separates Chinese and Latin text.
- Set a document word goal and see estimated reading time. Choose Chinese or English UI, font family, page width, text size, and line height.
- Paste CSV or tab-separated content as tables and sort table rows by a column.

### Documents and media

- Create documents from Space templates, with date, time, weekday, and title placeholders. Open or create today's note with one command.
- Find and replace across documents, rename or merge tags, and sort folders by name or modification time. The command bar remembers recent commands.
- Move a document into its own native window while preserving its cursor, scroll position, and undo history.
- Play animated GIFs and crop still images into a new asset while keeping the original. Export documents as standalone HTML pages alongside Markdown and PDF.
- Undo document deletion and moves from the notification. Refine sidebar, Space, and document navigation behavior.

### Editor performance

- Index UTF-16 and UTF-8 positions with Fenwick trees, reuse stable block IDs, and read only the relevant text ranges for ordinary input and word navigation.
- Update local layouts, footnote references, word counts, and search results incrementally. Cull offscreen rows and decorations, virtualize long outlines, and avoid full-document snapshots during repeated IME candidate updates.
- On the million-character mixed fixture, CPU scene construction is **0.273 ms P50 / 0.291 ms P95** for typing and **0.271 ms / 0.306 ms** for backspace. Typing with search open takes **0.281 ms P50**, including event handling; repeated IME candidate updates take **0.264 ms P50**, excluding composition start and commit.
- These measurements cover native event handling and CPU scene construction as specified in the [performance report](https://github.com/Ciao1019/ArcMD/blob/v0.0.11/docs/performance.md), not GPU presentation or end-to-end display latency. Existing editing interactions and full-layout fallbacks are retained.

### Documentation

- Refresh both READMEs with the current feature set and optimized performance results, without test dates.
- Add smooth, looping Chinese and English annotation SVGs, original demo documents, and a Go artwork generator.

### Data compatibility

ArcMD 0.0.11 writes document format **8** and Space configuration format **8**, migrating older formats when saved. Documents gain annotations, toggle blocks, and word goals; Space configuration gains folder sorting. Older releases cannot open the upgraded formats. Keep a copy of documents and configuration if you need to return to an earlier version.

### Downloads

Packages are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`. Signed update archives and manifests support in-app updates. The plugin marketplace and plugin execution runtimes remain unavailable.

## 0.0.9

This update fixes jumping while selecting text across screens and replaces the README demonstrations with smooth vector animations.

### Fixes

- Keep the selection endpoint under the pointer while scrolling, even when the pointer stays still. Dragging and releasing no longer pull the page back to the caret, including in typewriter mode ([#1](https://github.com/Ciao1019/ArcMD/issues/1)).
- Scroll smoothly at the viewport edges, preserve manual mouse-wheel scrolling, and stop selection scrolling on release or window focus loss.
- Copy selected text without changing the viewport or selection.
- Add regression coverage for touchpad scrolling, mouse-wheel animation, reverse selection, viewport boundaries, multiple clicks, and inset editors with the outline open.

### Documentation

- Replace the Chinese and English theme and bookmark demonstrations with lightweight, self-contained SVG animations in the README hero's style.
- Animate theme colors, multi-color gradients, translucent frosted material, and light/dark appearance continuously. Animate bookmark placement, outline navigation, and the all-bookmarks command bar.
- Keep seamless loops and reduced-motion illustrations, and include a reproducible vector-art generator.

### Data compatibility

Document, Space configuration, and preference formats are unchanged from 0.0.8.

### Downloads

Packages are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`. Signed update archives and manifests support in-app updates.

## 0.0.8

This update fixes the cursor position after pasting rich content and adds native-client demonstrations to the README.

### Fixes

- Place the cursor at the end of the pasted content, including the final wrapped line, so continued typing appends to the paste instead of appearing at the start of its last block.
- Find the final editable leaf when pasting tables or columns, including merged table cells, while preserving the destination's surrounding text and undo/redo selection.
- Add regression coverage for native rich text, HTML, Markdown, plain text, CRLF line endings, emoji, and narrow and wide editor layouts.

### Documentation

- Update the Chinese and English READMEs with animated SVG demonstrations of space switching, theme colors and gradients, translucency, and bookmarks, captured from the locally installed client.
- Include a separately labeled material illustration for background blur and tint, plus reduced-motion support and capture provenance.

### Data compatibility

Document, Space configuration, and preference formats are unchanged from 0.0.7.

### Downloads

Packages are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`. Signed update archives and manifests support in-app updates.

## 0.0.7

This release adds a guided tour that introduces ArcMD the first time it opens.

### Guided tour

- A short tour walks through creating spaces, theme colors, swiping between spaces, bookmarks, pinned documents, and favorites. A spotlight glides to the part of the window each step is about, and every step has a small animated illustration in the space's own colors.
- The step on theme colors opens the real theme editor and gently moves its colors, so the whole window shows the live preview. Nothing is saved: the space keeps its theme when the step ends.
- The tour appears once: on the first launch of a new installation, and on the first launch after updating from an earlier version. Finishing or skipping it keeps it from appearing again; quitting halfway shows it on the next launch.
- Use ← and → to move between steps, Enter to continue, and Esc to skip. Replay it at any time from the command bar ("新手教程") or Settings › About.
- The tour only points: it does not change any space or document, and it honors the system's reduced-motion preference.

### Data compatibility

Document and Space configuration formats are unchanged from 0.0.6. Preferences gain a `tourSeen` flag; older ArcMD releases ignore it.

### Downloads

Packages are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`. Signed update archives and manifests support in-app updates from earlier releases.

ArcMD remains an early preview. macOS packages use ad-hoc signing and are not notarized; Windows packages are unsigned. The plugin marketplace and plugin execution runtimes remain unavailable.

## 0.0.6

This release brings the full set of new writing and reading features, together with all security and data-safety fixes from 0.0.5.

### Writing and navigation

- Bookmark passages, rename bookmarks, find them in the outline and global command bar, and jump between documents. Formula bookmarks include rendered previews.
- Write inline and block TeX with live source previews, `$$` input, and formula clipboard exchange. Native formula rendering includes embedded KaTeX fonts and their licenses; it supports ArcMD's TeX subset.
- Add footnotes with automatic numbering, jump to their definitions, and return to the reference.
- Use superscript and subscript, including Markdown and HTML interchange.
- Copy and paste rich Markdown and HTML content, including tables and columns.
- Edit links in an anchored popover and preview local wiki-link targets on hover.
- Resize images with the pointer or keyboard and save their widths with the document. The image viewer supports continuous zoom and panning.
- Automatically pair Chinese and English brackets and quotes, wrap selected text, and delete empty pairs together. Pairing can be disabled in settings.
- Adjust reading font size and line height in preferences.
- Lock individual editors or split panes for read-only viewing.
- Edit columns with clickable empty areas, Tab navigation, and responsive stacking in narrow layouts.
- Click gaps between or after closed blocks to insert a paragraph.
- Use reorganized More and settings menus. Rename sidebar documents and folders from their context menu; double-click no longer starts renaming.

### Security and data safety

All five fixes from 0.0.5 remain included: save-conflict read failures preserve unsaved edits; Space paths reject escaping symbolic links and stop folder-link cycles; malformed table cells produce load errors instead of panics; case-only renames cannot overwrite a different existing file; and plugin requests require HTTPS at every redirect. Regression tests, document-decoder fuzz coverage, and Linux case-sensitive filesystem checks are included.

### Data compatibility

ArcMD 0.0.6 writes document format **6** and Space configuration format **7**. It reads and migrates older formats. Documents are written in the new format when saved, and Space configuration is upgraded when persisted. Older ArcMD releases cannot read these upgraded files. Back up documents and configuration before upgrading if you need to return to an older release.

### Downloads

Packages are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`. Signed update archives, delta patches, and five `update-*.json` manifests support in-app updates.

ArcMD remains an early preview. macOS packages use ad-hoc signing and are not notarized; Windows packages are unsigned. Windows 32-bit, the plugin marketplace, and plugin execution runtimes remain unavailable. Keep `.arcmd` originals to preserve all rich-text features.

## 0.0.5

A security and data-safety patch release. Existing editing features, UI, and the document format remain unchanged from 0.0.4.

- Stop saving when the on-disk document cannot be read for conflict checking. Unsaved edits remain available, and external changes are not overwritten after a read failure.
- Reject document paths whose symbolic links resolve outside their Space, including paths for new files. Preserve valid links within a Space and stop recursive folder-link cycles. Relative Markdown images such as `../images/` remain supported.
- Reject malformed table cells with a document-load error instead of panicking. Damaged documents remain untouched on disk.
- Refuse case-only renames when the destination is a different existing file on a case-sensitive filesystem. Normal case-only renames remain supported.
- Require HTTPS for plugin index and package requests, including every redirect. The plugin marketplace and runtime remain unavailable in this release.
- Add regression coverage, document-decoder fuzz testing, and Linux CI coverage for case-sensitive file operations.

Downloads are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`.

## 0.0.4

A macOS application-icon correction for the hand-drawn beaver identity.

- Normalize the icon tile to the macOS grid and make its interior opaque, so macOS Tahoe no longer shrinks it inside an extra gray background.
- Preserve the approved beaver illustration and ivory paper background. The horizontal README artwork is unchanged.
- Application features remain the same as 0.0.2; unreleased plugin work is not included.

## 0.0.3

A logo-only release introducing ArcMD’s hand-drawn beaver identity.

- Replace the application icon with the new beaver artwork, including dedicated small-size macOS icons.
- Use the horizontal ArcMD logo in the Chinese and English READMEs.
- Application features remain the same as 0.0.2. The plugin marketplace/runtime work is not included.

Downloads are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`.

## 0.0.2

This release improves images, document moves, system file opening, and PDF export.

### Improvements

- Open ArcMD and Markdown documents through system file associations, Dock drops, or development command-line arguments. Files reuse the closest existing space, or add their parent folder as a space, without converting or copying the original on open.
- Display images at their natural proportions, with a full-size preview and editable descriptions. Missing or unreadable images now show recovery actions to choose a replacement or locate the original; replacements support undo and redo.
- Load local Markdown images from parent folders, including URL-encoded filenames and GIF first frames.
- Export PDFs page by page with progress and cancellation, keeping memory use bounded. Text and table content can now be selected, copied, and searched in the exported PDF.

### Fixes

- Keep images, covers, undo/redo states, and version history together when moving documents or folders, including across spaces. Shared images remain available, filename collisions are handled without overwriting, and resource failures roll back the move.
- Resolve wiki links in the source document's space first. Renaming a document updates links that resolved to it without changing links to same-named documents in other spaces.
- Keep toolbar controls clear of native window buttons in normal, focus, and split views, including narrow windows and long document titles.

### Downloads

| Platform | Architecture | Packages |
| --- | --- | --- |
| macOS | Apple Silicon | `arcmd-0.0.2-arm64.dmg` |
| macOS | Intel | `arcmd-0.0.2-amd64.dmg` |
| Windows | x64 | `arcmd-setup-0.0.2-amd64.exe`, `arcmd-0.0.2-windows-amd64.zip` |
| Linux | x64 | `arcmd_0.0.2_amd64.deb`, `arcmd-0.0.2-linux-amd64.tar.gz` |
| Linux | ARM64 | `arcmd_0.0.2_arm64.deb`, `arcmd-0.0.2-linux-arm64.tar.gz` |

Verify downloads against `SHA256SUMS.txt`. The additional `update-*.json` manifests and signed `.tar.gz` archives support application updates.

### Preview limitations

ArcMD remains an early preview. macOS packages use ad-hoc signing and are not notarized; Windows packages are unsigned. Windows 32-bit and the plugin marketplace/runtime are not yet available. GIFs display their first frame, and PDF formulas and images remain raster content. Keep `.arcmd` originals to preserve all rich-text features.

## 0.0.1

The first ArcMD release: a native rich-text editor built in Go, inspired by Arc.

- Organize local documents in colorful spaces with favorites, pinned documents, folders, and search.
- Reorder spaces by dragging their cards in the library or using the arrow keys; the space switcher follows the saved order.
- Write with headings, lists, tasks, tables, code blocks, images, callouts, and columns.
- Navigate with the command palette, document outlines, links, and up to four split panes.
- Import and export Markdown while keeping full document structure in local `.arcmd` files.
- Keep work with autosave, version history, external-change detection, and recently deleted items.
- Receive signed application updates through GitHub Releases.

### Downloads

| Platform | Architecture | Packages |
| --- | --- | --- |
| macOS | Apple Silicon | `arcmd-0.0.1-arm64.dmg` |
| macOS | Intel | `arcmd-0.0.1-amd64.dmg` |
| Windows | x64 | `arcmd-setup-0.0.1-amd64.exe`, `arcmd-0.0.1-windows-amd64.zip` |
| Linux | x64 | `arcmd_0.0.1_amd64.deb`, `arcmd-0.0.1-linux-amd64.tar.gz` |
| Linux | ARM64 | `arcmd_0.0.1_arm64.deb`, `arcmd-0.0.1-linux-arm64.tar.gz` |

Verify downloads against `SHA256SUMS.txt`. The additional `update-*.json` manifests and signed `.tar.gz` archives support application updates.

### Preview limitations

ArcMD is an early preview; macOS is the primary development platform. macOS packages use ad-hoc signing and are not notarized, and Windows packages are unsigned. Update signatures verify application updates; they do not replace operating-system code signing.

Windows 32-bit is unavailable because MyGo v0.7.4 has no working GUI backend for it. The plugin marketplace and plugin execution runtime are not yet available. Markdown export cannot preserve every rich-text feature; retain `.arcmd` files as the complete originals.
