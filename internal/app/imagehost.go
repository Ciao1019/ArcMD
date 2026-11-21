package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/plugin"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
)

type pendingImage struct {
	name        string
	alt         string
	contentType string
	data        []byte
}

type uploadImageResult struct {
	item pendingImage
	url  string
	err  error
}

func boolSetting(cfg map[string]any, key string, def bool) bool {
	if cfg == nil {
		return def
	}
	v, ok := cfg[key]
	if !ok {
		return def
	}
	b, ok := v.(bool)
	if !ok {
		return def
	}
	return b
}

func (a *App) activeImageHost() (*plugin.Installed, map[string]any) {
	p := a.findInstalledPlugin(imageHostPluginID)
	if p == nil || !p.Enabled {
		return nil, nil
	}
	cfg := a.pluginConfig(p)
	if cfg == nil {
		cfg = map[string]any{}
	}
	return p, cfg
}

func (a *App) imageHostAutoUploadEnabled() bool {
	_, cfg := a.activeImageHost()
	if cfg == nil {
		return false
	}
	return boolSetting(cfg, "autoUpload", true)
}

func invokeImageHostUpload(ctx context.Context, inst plugin.Instance, item pendingImage, spaceID string) (string, error) {
	if len(item.data) == 0 {
		return "", errors.New(i18n.T("图片内容为空"))
	}
	resp, err := inst.Invoke(ctx, plugin.Request{
		Command:     "upload-image",
		ActiveSpace: spaceID,
		Args: map[string]any{
			"filename":    item.name,
			"contentType": item.contentType,
			"dataBase64":  base64.StdEncoding.EncodeToString(item.data),
		},
	})
	if err != nil {
		return "", err
	}
	if resp == nil || resp.Data == nil {
		return "", errors.New(i18n.T("图床插件未返回图片地址"))
	}
	rawURL, _ := resp.Data["url"].(string)
	rawURL = strings.TrimSpace(rawURL)
	if !isRemoteImageURL(rawURL) {
		return "", fmt.Errorf(i18n.T("图床返回无效地址 %q"), rawURL)
	}
	return rawURL, nil
}

// uploadImagesViaHost uploads pasted or dropped images through the active
// image-host plugin without writing anything inside the Space folder, and
// inserts remote image URLs into d (falling back locally only if configured).
func (a *App) uploadImagesViaHost(d *document, items []pendingImage, at int) {
	if d == nil || d.loadErr != nil || d.source || d.ed.ReadOnly || len(items) == 0 {
		return
	}
	p, cfg := a.activeImageHost()
	if p == nil {
		return
	}
	fallback := boolSetting(cfg, "fallbackLocal", true)
	st := a.pluginRunState(p.Manifest.ID)

	finishLocalFallback := func(reason error) {
		if !fallback {
			a.notify("上传图片到图床失败。%v", reason)
			return
		}
		var blocks []*model.Block
		a.withDocument(d, func() {
			for _, it := range items {
				rel, err := a.writeAssetFor(d, path.Dir(d.rel), it.name, it.data)
				if err != nil {
					a.notify("保存图片 %s 失败。%v", it.name, err)
					continue
				}
				blocks = append(blocks, imageBlock(it.alt, rel))
			}
			a.placeImages(blocks, at)
		})
		if len(blocks) > 0 {
			a.notify("图床上传失败，已回退保存到本地：%v", reason)
		}
	}

	inst, err := a.ensurePluginInstance(p)
	if err != nil {
		st.err = err.Error()
		finishLocalFallback(err)
		return
	}

	spaceID := ""
	if d.space != nil {
		spaceID = d.space.ID
	}

	apply := func(results []uploadImageResult) {
		if d.closed.Load() || d.loadErr != nil || d.source || d.ed.ReadOnly || a.residentDocument(d.space.ID, d.rel) != d {
			return
		}
		var blocks []*model.Block
		var uploaded, fallbacks int
		var lastErr error
		a.withDocument(d, func() {
			for _, res := range results {
				if res.err == nil && res.url != "" {
					_, _ = a.cacheRemoteImageBytes(res.url, res.item.data)
					blocks = append(blocks, imageBlock(res.item.alt, res.url))
					uploaded++
					continue
				}
				lastErr = res.err
				if fallback {
					rel, werr := a.writeAssetFor(d, path.Dir(d.rel), res.item.name, res.item.data)
					if werr != nil {
						a.notify("保存图片 %s 失败。%v", res.item.name, werr)
						continue
					}
					blocks = append(blocks, imageBlock(res.item.alt, rel))
					fallbacks++
					continue
				}
				a.notify("上传图片 %s 失败。%v", res.item.name, res.err)
			}
			a.placeImages(blocks, at)
		})
		switch {
		case uploaded > 0 && fallbacks == 0:
			st.err = ""
			st.status = fmt.Sprintf(i18n.T("已上传 %d 张图片"), uploaded)
			a.notify("已上传 %d 张图片到图床。", uploaded)
		case fallbacks > 0 && lastErr != nil:
			st.err = lastErr.Error()
			a.notify("图床上传失败，已回退保存到本地：%v", lastErr)
		case lastErr != nil:
			st.err = lastErr.Error()
		}
	}

	uploadAll := func() []uploadImageResult {
		results := make([]uploadImageResult, len(items))
		for i, it := range items {
			u, uerr := invokeImageHostUpload(context.Background(), inst, it, spaceID)
			results[i] = uploadImageResult{item: it, url: u, err: uerr}
		}
		return results
	}

	post := a.asyncPost()
	if a.pluginsUI.sync || post == nil {
		apply(uploadAll())
		return
	}
	go func() {
		results := uploadAll()
		post(func() { apply(results) })
	}()
}

// uploadCurrentDocumentImages uploads all local images in the open document
// via dev.arcmd.image-host, rewrites their URLs in one undoable edit, and
// optionally moves unreferenced local files to Trash.
func (a *App) uploadCurrentDocumentImages(p *plugin.Installed) {
	if p == nil || p.Manifest == nil {
		return
	}
	d := a.doc
	if d == nil || d.loadErr != nil || d.isWhiteboard() || d.isPDF() || d.source || d.ed.ReadOnly {
		a.notify("请先打开一篇可编辑的文档。")
		return
	}
	st := a.pluginRunState(p.Manifest.ID)
	if st.busy {
		return
	}
	inst, err := a.ensurePluginInstance(p)
	if err != nil {
		st.err = err.Error()
		a.notify("%v", err)
		return
	}
	cfg := a.pluginConfig(p)
	deleteLocal := boolSetting(cfg, "deleteLocalAfterMigrate", true)

	type localItem struct {
		url      string
		fullPath string
		data     []byte
		name     string
	}
	var items []localItem
	seen := map[string]bool{}
	for _, leaf := range d.ed.Doc.Leaves() {
		if leaf.Block.Kind != "image" {
			continue
		}
		u := strings.TrimSpace(leaf.Block.URL)
		if u == "" || seen[u] || isRemoteImageURL(u) || strings.HasPrefix(u, "data:") ||
			editor.IsPDFURL(u) || vault.IsWhiteboard(u) {
			continue
		}
		seen[u] = true
		full, rerr := d.localImage(u)
		if rerr != nil {
			continue
		}
		data, ferr := os.ReadFile(full)
		if ferr != nil || len(data) == 0 {
			continue
		}
		items = append(items, localItem{
			url:      u,
			fullPath: full,
			data:     data,
			name:     filepath.Base(full),
		})
	}
	if len(items) == 0 {
		msg := i18n.T("当前文档没有需要上传的本地图片。")
		st.status = msg
		a.notify("%s", msg)
		return
	}

	spaceID := ""
	if d.space != nil {
		spaceID = d.space.ID
	}

	st.busy = true
	st.err = ""

	type migrateResult struct {
		item localItem
		url  string
		err  error
	}

	finish := func(results []migrateResult) {
		st.busy = false
		if d.closed.Load() || d.loadErr != nil || d.source || d.ed.ReadOnly || a.residentDocument(d.space.ID, d.rel) != d {
			return
		}
		replacements := make(map[string]string, len(results))
		var migrated []migrateResult
		var firstErr error
		for _, r := range results {
			if r.err != nil {
				if firstErr == nil {
					firstErr = r.err
				}
				continue
			}
			_, _ = a.cacheRemoteImageBytes(r.url, r.item.data)
			replacements[r.item.url] = r.url
			migrated = append(migrated, r)
		}
		if len(replacements) == 0 {
			if firstErr != nil {
				st.err = firstErr.Error()
				a.notify("上传本地图片失败。%v", firstErr)
			}
			return
		}
		a.withDocument(d, func() {
			d.ed.ReplaceImageURLs(replacements)
		})
		a.invalidateResolvedImages()

		trashed := 0
		if deleteLocal && d.space != nil && !d.space.IsExternal() {
			rootEval := canonicalPath(d.space.Root)
			trashedSet := map[string]bool{}
			for _, m := range migrated {
				targetEval := canonicalPath(m.item.fullPath)
				if trashedSet[targetEval] {
					continue
				}
				if a.countLocalImageReferences(d.space, targetEval) > 0 {
					continue
				}
				relToRoot, relErr := filepath.Rel(rootEval, targetEval)
				if relErr != nil || !filepath.IsLocal(relToRoot) {
					continue
				}
				relSlash := filepath.ToSlash(relToRoot)
				kept, ok := a.keepDeleted(d.space.ID, d.space.Name, d.space.Root, relSlash)
				if terr := a.trashItem(m.item.fullPath); terr != nil {
					if ok {
						a.bin.Forget(kept)
					}
					continue
				}
				trashedSet[targetEval] = true
				trashed++
			}
		}

		failed := len(results) - len(migrated)
		var msg string
		switch {
		case failed > 0:
			msg = fmt.Sprintf(i18n.T("已上传 %d 张图片到图床，%d 张失败：%v"), len(migrated), failed, firstErr)
			st.err = firstErr.Error()
		case trashed > 0:
			msg = fmt.Sprintf(i18n.T("已上传 %d 张图片到图床，并清理 %d 个本地文件。"), len(migrated), trashed)
		default:
			msg = fmt.Sprintf(i18n.T("已上传 %d 张图片到图床。"), len(migrated))
		}
		st.status = msg
		a.notify("%s", msg)
	}

	run := func() []migrateResult {
		out := make([]migrateResult, len(items))
		for i, it := range items {
			u, uerr := invokeImageHostUpload(context.Background(), inst, pendingImage{
				name: it.name,
				data: it.data,
			}, spaceID)
			out[i] = migrateResult{item: it, url: u, err: uerr}
		}
		return out
	}

	post := a.asyncPost()
	if a.pluginsUI.sync || post == nil {
		finish(run())
		return
	}
	go func() {
		res := run()
		post(func() { finish(res) })
	}()
}

func canonicalPath(p string) string {
	abs, err := filepath.Abs(p)
	if err == nil {
		p = abs
	}
	if eval, err := filepath.EvalSymlinks(p); err == nil {
		return eval
	}
	return p
}

func (a *App) countLocalImageReferences(sp *space.Space, targetEval string) int {
	if sp == nil || sp.IsExternal() {
		return 0
	}
	count := 0
	checkDoc := func(rel string, doc *model.Document) {
		if doc == nil {
			return
		}
		checkURL := func(raw string) {
			if raw == "" || isRemoteImageURL(raw) || strings.HasPrefix(raw, "data:") {
				return
			}
			if full, err := resolveSpaceImage(sp, rel, raw); err == nil && canonicalPath(full) == targetEval {
				count++
			}
		}
		checkURL(doc.Cover)
		var walk func([]*model.Block)
		walk = func(blocks []*model.Block) {
			for _, b := range blocks {
				if b.Kind == "image" {
					checkURL(b.URL)
				}
				walk(b.Children)
			}
		}
		walk(doc.Blocks)
	}

	resident := map[string]bool{}
	for _, d := range a.residentDocuments() {
		if d.space != sp || d.loadErr != nil {
			continue
		}
		resident[d.rel] = true
		if d.ed != nil {
			checkDoc(d.rel, d.ed.Doc)
		}
	}

	docs, err := vault.List(sp.Root)
	if err != nil {
		return count
	}
	for _, entry := range docs {
		if resident[entry.Path] || vault.IsWhiteboard(entry.Path) || vault.IsPDF(entry.Path) {
			continue
		}
		text, rerr := vault.Read(sp.Root, entry.Path)
		if rerr != nil {
			continue
		}
		if doc, derr := imageDocument(entry.Path, text); derr == nil {
			checkDoc(entry.Path, doc)
		}
	}
	return count
}
