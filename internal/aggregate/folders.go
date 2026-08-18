package aggregate

import (
	"path/filepath"
	"sort"

	"disk-report/internal/schema"
)

// DirAccum is a directory's running total while the walk is in flight.
type DirAccum struct {
	Path   string
	Parent string
	// HasParent distinguishes "not linked yet" from "linked to the empty path".
	HasParent    bool
	OwnSize      int64
	OwnFileCount int64
	MaxMtimeMs   float64
	IsCloud      bool
	// Redundant bytes among this directory's own files, filled after the walk.
	DuplicateOwnSize int64
	Children         []string
}

// Dirs is the flat set of per-directory accumulators the folder tree is built
// from.
type Dirs map[string]*DirAccum

// Ensure returns the accumulator for a directory, creating it if the walk has
// not reached it yet.
func (d Dirs) Ensure(path string, isCloud bool) *DirAccum {
	accum, ok := d[path]
	if !ok {
		accum = &DirAccum{Path: path, IsCloud: isCloud}
		d[path] = accum
	}
	return accum
}

// LinkChild attaches a discovered subdirectory to its parent, once.
func (d Dirs) LinkChild(childPath string, isCloud bool) {
	child := d.Ensure(childPath, isCloud)
	if child.HasParent {
		return
	}

	parentPath := filepath.Dir(childPath)
	if parentPath == childPath {
		return
	}
	parent, ok := d[parentPath]
	if !ok {
		return
	}

	child.Parent = parentPath
	child.HasParent = true
	parent.Children = append(parent.Children, childPath)
}

// LinkOrphans gives a parent to directories that were discovered but never
// scanned — excluded, or a failed read — so the tree has no holes.
func (d Dirs) LinkOrphans(roots []string) {
	isRoot := make(map[string]bool, len(roots))
	for _, root := range roots {
		isRoot[root] = true
	}

	for _, accum := range d {
		if accum.HasParent || isRoot[accum.Path] {
			continue
		}
		parent, ok := d[filepath.Dir(accum.Path)]
		if !ok || contains(parent.Children, accum.Path) {
			continue
		}
		accum.Parent = parent.Path
		accum.HasParent = true
		parent.Children = append(parent.Children, accum.Path)
	}
}

// rollup holds the bottom-up totals for one directory.
type rollup struct {
	size      int64
	files     int64
	mtimeMs   float64
	duplicate int64
}

// BuildFolderTree assembles the tree from flat accumulators.
//
// Recursive sizes are computed bottom-up with an explicit stack: home
// directories nest deeply enough (node_modules chains, Xcode caches) that the
// depth is not worth trusting to the call stack.
//
// Children below minFolderSize are pruned into TruncatedChildCount and
// TruncatedSize, so a folder always accounts for its full weight even when its
// small children are not listed.
func BuildFolderTree(dirs Dirs, roots []string, minFolderSize int64) schema.FolderNode {
	totals := make(map[string]rollup, len(dirs))

	presentRoots := make([]string, 0, len(roots))
	for _, root := range roots {
		if _, ok := dirs[root]; ok {
			presentRoots = append(presentRoots, root)
		}
	}

	type frame struct {
		path    string
		visited bool
	}

	// Post-order: a node is finalized only after all of its children.
	for _, root := range presentRoots {
		stack := []frame{{path: root}}
		for len(stack) > 0 {
			current := stack[len(stack)-1]
			stack = stack[:len(stack)-1]

			accum, ok := dirs[current.path]
			if !ok {
				continue
			}

			if !current.visited {
				stack = append(stack, frame{path: current.path, visited: true})
				for _, child := range accum.Children {
					stack = append(stack, frame{path: child})
				}
				continue
			}

			total := rollup{
				size:      accum.OwnSize,
				files:     accum.OwnFileCount,
				mtimeMs:   accum.MaxMtimeMs,
				duplicate: accum.DuplicateOwnSize,
			}
			for _, child := range accum.Children {
				childTotal := totals[child]
				total.size += childTotal.size
				total.files += childTotal.files
				total.duplicate += childTotal.duplicate
				if childTotal.mtimeMs > total.mtimeMs {
					total.mtimeMs = childTotal.mtimeMs
				}
			}
			totals[current.path] = total
		}
	}

	builder := &treeBuilder{dirs: dirs, totals: totals, minFolderSize: minFolderSize}

	if len(presentRoots) == 1 {
		return builder.node(presentRoots[0])
	}

	children := make([]schema.FolderNode, 0, len(presentRoots))
	for _, root := range presentRoots {
		children = append(children, builder.node(root))
	}

	// Several roots need a synthetic parent, so the tree still has one top.
	combined := schema.FolderNode{Path: "", Name: "roots", Children: children}
	for _, child := range children {
		combined.RecursiveSize += child.RecursiveSize
		combined.FileCount += child.FileCount
		combined.DuplicateRecursiveSize += child.DuplicateRecursiveSize
		if child.MaxMtimeMs > combined.MaxMtimeMs {
			combined.MaxMtimeMs = child.MaxMtimeMs
		}
	}
	sortNodes(combined.Children)
	return combined
}

// treeBuilder turns finished rollups into nodes.
//
// The descent is recursive where the rollup pass was not: pruning at
// minFolderSize bounds the depth long before it becomes a concern, and a
// goroutine stack grows on demand anyway.
type treeBuilder struct {
	dirs          Dirs
	totals        map[string]rollup
	minFolderSize int64
}

func (b *treeBuilder) node(path string) schema.FolderNode {
	accum := b.dirs[path]
	total := b.totals[path]

	name := filepath.Base(path)
	if name == "" {
		name = path
	}

	node := schema.FolderNode{
		Path:                   path,
		Name:                   name,
		RecursiveSize:          total.size,
		OwnSize:                accum.OwnSize,
		FileCount:              total.files,
		OwnFileCount:           accum.OwnFileCount,
		MaxMtimeMs:             total.mtimeMs,
		IsCloud:                accum.IsCloud,
		DuplicateOwnSize:       accum.DuplicateOwnSize,
		DuplicateRecursiveSize: total.duplicate,
		Children:               []schema.FolderNode{},
	}

	for _, childPath := range accum.Children {
		childSize := b.totals[childPath].size
		if childSize >= b.minFolderSize {
			node.Children = append(node.Children, b.node(childPath))
			continue
		}
		node.TruncatedChildCount++
		node.TruncatedSize += childSize
	}

	sortNodes(node.Children)
	return node
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// sortNodes orders biggest first, with a path tiebreak so equal-sized siblings
// keep a fixed order between runs.
func sortNodes(nodes []schema.FolderNode) {
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].RecursiveSize != nodes[j].RecursiveSize {
			return nodes[i].RecursiveSize > nodes[j].RecursiveSize
		}
		return nodes[i].Path < nodes[j].Path
	})
}
