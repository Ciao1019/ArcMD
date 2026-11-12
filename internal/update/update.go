// Package update holds what ArcMD's software updates decide without the
// window: what the sidebar's update banner says and when that goes into a
// toast (after Cra's UpdateNotice, MIT), how often the releases are
// checked, and the release notes made plain for the update window. The
// downloading and replacing of the app is MyGo's Updater; this package has
// no dependency on it.
package update

import (
	"regexp"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/i18n"
)

const (
	// FirstCheck is how long after launch the first background check runs,
	// so that it does not slow the start.
	FirstCheck = 10 * time.Second
	// Interval is the time between background checks while ArcMD runs, as
	// Arc's and Cra's eight hours.
	Interval = 8 * time.Hour
)

// Repository is where releases are published.
const Repository = "https://github.com/Ciao1019/ArcMD"

// ReleaseURL is the page of a release on GitHub, or of the latest one when
// version is empty.
func ReleaseURL(version string) string {
	if version == "" {
		return Repository + "/releases/latest"
	}
	return Repository + "/releases/tag/v" + strings.TrimPrefix(version, "v")
}

// Kind is what a notice is about.
type Kind int

const (
	// Ready is an update downloaded and one restart away.
	Ready Kind = iota + 1
	// Available is an update found while automatic updates are off.
	Available
)

// Notice is what the update banner says (Arc's SidebarBanner
// updateAvailable), and the toast for a window whose sidebar is hidden.
type Notice struct {
	Kind    Kind
	Version string
}

// Current is the banner's news: a downloaded update before a found one,
// nothing once its ✕ was pressed, which hides it until the next launch as
// in Arc.
func Current(ready, found string, dismissed bool) *Notice {
	switch {
	case dismissed:
		return nil
	case ready != "":
		return &Notice{Ready, ready}
	case found != "":
		return &Notice{Available, found}
	}
	return nil
}

// Title is the banner's title, also the toast's text.
func (n Notice) Title() string {
	if n.Kind == Ready {
		return i18n.T("更新已准备就绪！")
	}
	return i18n.T("ArcMD 有新版本")
}

// Subtitle is the banner's second line.
func (n Notice) Subtitle() string {
	if n.Kind == Ready {
		return i18n.T("点击重新启动")
	}
	return "ArcMD " + n.Version
}

// Button is the toast's button: what clicking the banner does.
func (n Notice) Button() string {
	if n.Kind == Ready {
		return i18n.T("重新启动")
	}
	return i18n.T("更新")
}

// Toast returns the notice to toast, or nil: only while the sidebar, which
// carries the banner, is hidden, and once a launch for each notice (told is
// the last one toasted), so that hiding and showing the sidebar does not
// bring it back. A later notice, such as the found update finishing its
// download or a newer release, is news again.
func Toast(n *Notice, told *Notice, sidebarVisible bool) *Notice {
	if n == nil || sidebarVisible || told != nil && *told == *n {
		return nil
	}
	return n
}

// Due reports whether a background check is due at now, given when the
// next one was planned; a zero plan is the launch's first check.
func Due(next, now time.Time) bool { return !next.IsZero() && !now.Before(next) }

var (
	notesLink   = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	notesMarks  = regexp.MustCompile("\\*\\*|__|`")
	notesBullet = regexp.MustCompile(`^\s*([-*+]|\d+[.)])\s+`)
	notesBare   = regexp.MustCompile(`https?://\S+`)
)

// Lines turns release notes in Markdown into plain lines for the update
// window: headings as they are, list items with a bullet, links as their
// text, emphasis and code marks dropped, and runs of blank lines as one.
func Lines(notes string) []string {
	var out []string
	blank := true
	for _, line := range strings.Split(strings.ReplaceAll(notes, "\r\n", "\n"), "\n") {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			if !blank {
				out = append(out, "")
			}
			blank = true
			continue
		}
		blank = false
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "#"):
			line = strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		case notesBullet.MatchString(line):
			line = "• " + notesBullet.ReplaceAllString(line, "")
		default:
			line = trimmed
		}
		line = notesLink.ReplaceAllString(line, "$1")
		line = notesMarks.ReplaceAllString(line, "")
		line = strings.TrimSpace(notesBare.ReplaceAllStringFunc(line, func(u string) string {
			// GitHub's generated notes end items with the pull request's
			// address: its number reads better.
			if i := strings.LastIndex(u, "/pull/"); i >= 0 {
				return "#" + u[i+len("/pull/"):]
			}
			return u
		}))
		out = append(out, line)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}
