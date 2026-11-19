//go:generate go run ./gen_bundle.go

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	in, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read stdin: %v\n", err)
		os.Exit(1)
	}
	var req pluginRequest
	if len(in) > 0 {
		if err := json.Unmarshal(in, &req); err != nil {
			fmt.Fprintf(os.Stderr, "decode request: %v\n", err)
			os.Exit(1)
		}
	}
	resp, err := handleRequest(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	out, err := json.Marshal(resp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "encode response: %v\n", err)
		os.Exit(1)
	}
	_, _ = os.Stdout.Write(out)
}

func handleRequest(req pluginRequest) (pluginResponse, error) {
	cfg, err := sdkGetConfig()
	if err != nil {
		return pluginResponse{}, err
	}

	switch {
	case req.Command == "test-connection":
		backend, err := backendFromConfig(cfg)
		if err != nil {
			return pluginResponse{
				Toast:  err.Error(),
				Status: "连接失败：" + err.Error(),
			}, nil
		}
		if err := backend.Test(); err != nil {
			return pluginResponse{
				Toast:  "连接失败：" + err.Error(),
				Status: "连接失败：" + err.Error(),
			}, nil
		}
		msg := fmt.Sprintf("%s 连接成功。", backend.Name())
		return pluginResponse{
			Toast:  msg,
			Status: msg,
		}, nil

	case req.Command == "sync-now":
		trigger, _ := req.Args["trigger"].(string)
		if trigger == "" {
			trigger = "manual"
		}
		rec, err := runSync(cfg, trigger)
		summary := formatSyncSummary(rec)
		if err != nil {
			return pluginResponse{
				Toast:  "云端同步部分失败：" + err.Error(),
				Status: "同步失败：" + err.Error(),
			}, nil
		}
		return pluginResponse{
			Toast:  summary,
			Status: summary,
		}, nil

	case req.Command == "sync-status" || req.Panel == "status":
		return pluginResponse{
			Markdown: renderStatusMarkdown(cfg),
			Status:   latestSyncStatusLine(),
		}, nil

	default:
		return pluginResponse{}, fmt.Errorf("unknown command %q / panel %q", req.Command, req.Panel)
	}
}

func formatSyncSummary(rec syncRecord) string {
	var parts []string
	if rec.Merged > 0 {
		parts = append(parts, fmt.Sprintf("自动合并 %d", rec.Merged))
	}
	if rec.Uploaded > 0 {
		parts = append(parts, fmt.Sprintf("上传 %d", rec.Uploaded))
	}
	if rec.Downloaded > 0 {
		parts = append(parts, fmt.Sprintf("下载 %d", rec.Downloaded))
	}
	if rec.DeletedLocal+rec.DeletedRemote > 0 {
		parts = append(parts, fmt.Sprintf("删除 %d", rec.DeletedLocal+rec.DeletedRemote))
	}
	if rec.Conflicts > 0 {
		parts = append(parts, fmt.Sprintf("待合并冲突 %d", rec.Conflicts))
	}
	if rec.Skipped > 0 {
		parts = append(parts, fmt.Sprintf("跳过未保存 %d", rec.Skipped))
	}
	if len(parts) == 0 {
		return "云端同步完成：已是最新"
	}
	return "云端同步完成：" + strings.Join(parts, "，")
}

func latestSyncStatusLine() string {
	hist := loadSyncHistory()
	if len(hist) == 0 {
		return "尚未同步"
	}
	last := hist[0]
	if last.Error != "" {
		return "上次同步失败：" + last.Error
	}
	return formatSyncSummary(last)
}

func renderStatusMarkdown(cfg map[string]any) string {
	var b strings.Builder
	provider := strings.ToUpper(strCfg(cfg, "provider"))
	if provider == "" {
		provider = "S3"
	}
	spaces := strSliceCfg(cfg, "spaces")
	b.WriteString("# 云端同步记录\n\n")
	fmt.Fprintf(&b, "- **同步方式**: %s\n", provider)
	fmt.Fprintf(&b, "- **已选空间数**: %d\n\n", len(spaces))

	hist := loadSyncHistory()
	if len(hist) == 0 {
		b.WriteString("还没有同步记录。请在插件设置中配置连接参数并勾选需要同步的空间，然后点击 **立即同步**。\n")
		return b.String()
	}
	b.WriteString("| 时间 | 方式 | 触发 | 空间 | 结果 |\n")
	b.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, r := range hist {
		res := formatSyncSummary(r)
		if r.Error != "" {
			res = "失败: " + strings.ReplaceAll(r.Error, "|", "/")
		}
		trig := "手动"
		switch r.Trigger {
		case "auto":
			trig = "定时"
		case "save":
			trig = "保存后"
		}
		spNames := strings.Join(r.Spaces, ", ")
		if spNames == "" {
			spNames = "-"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", r.Time, strings.ToUpper(r.Provider), trig, spNames, res)
	}
	return b.String()
}
