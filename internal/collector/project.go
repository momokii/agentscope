package collector

import (
	"path/filepath"
	"sort"

	"opencode-dashboard/internal/model"
	"opencode-dashboard/internal/ports"
)

// buildProjects aggregates sessions by directory and joins the runtime
// source's per-directory live process and live pane counts. Directories with
// no sessions are excluded (there is no project row to describe) and a
// session directory with no live process reports live_procs 0. Sort order is
// the frozen contract: live_procs desc, live_panes desc, running desc,
// last_activity_ms asc, directory asc.
func buildProjects(rows []model.SessionRow, procsByDir, panesByDir map[string]int) []model.ProjectSummary {
	byDir := make(map[string]*model.ProjectSummary)
	lastActivity := make(map[string]int64)
	haveActivity := make(map[string]bool)

	for i := range rows {
		if rows[i].Directory == "" {
			continue
		}
		key := filepath.Clean(rows[i].Directory)
		project, ok := byDir[key]
		if !ok {
			project = &model.ProjectSummary{Directory: key, Name: projectName(key)}
			byDir[key] = project
		}
		project.Sessions++
		switch rows[i].Status {
		case model.SessionRunning:
			project.Running++
		case model.SessionWaitingInput:
			project.WaitingInput++
		default:
			project.Idle++
		}
		project.TokensTotal += rows[i].RawTokensTotal
		project.CostTotal += rows[i].RawCost
		if !haveActivity[key] || rows[i].UpdatedRelativeMs < lastActivity[key] {
			lastActivity[key] = rows[i].UpdatedRelativeMs
			haveActivity[key] = true
		}
	}

	out := make([]model.ProjectSummary, 0, len(byDir))
	for key, project := range byDir {
		project.LastActivityMs = lastActivity[key]
		project.LiveProcs = procsByDir[key]
		project.LivePanes = panesByDir[key]
		out = append(out, *project)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].LiveProcs != out[j].LiveProcs {
			return out[i].LiveProcs > out[j].LiveProcs
		}
		if out[i].LivePanes != out[j].LivePanes {
			return out[i].LivePanes > out[j].LivePanes
		}
		if out[i].Running != out[j].Running {
			return out[i].Running > out[j].Running
		}
		if out[i].LastActivityMs != out[j].LastActivityMs {
			return out[i].LastActivityMs < out[j].LastActivityMs
		}
		return out[i].Directory < out[j].Directory
	})
	return out
}

// paneDirSource is the optional runtime capability carrying pane presence per
// directory. Runtime sources that cannot observe panes (or test stand-ins
// that predate the capability) simply omit it; projects then report 0.
type paneDirSource interface {
	PanesByDir() map[string]int
}

func panesByDir(rt ports.RuntimeSource) map[string]int {
	if src, ok := rt.(paneDirSource); ok {
		return src.PanesByDir()
	}
	return nil
}

// projectName is the display label for a directory: its basename, or the
// directory itself when it has none (root / empty after cleaning).
func projectName(dir string) string {
	base := filepath.Base(dir)
	if base == "." || base == "" || base == string(filepath.Separator) {
		return dir
	}
	return base
}
