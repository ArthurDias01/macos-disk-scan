package scan

import "disk-report/internal/duplicates"

// ApplyDuplicates folds the second pass back into the walk's results.
//
// Sizes are deliberately not rewritten. A byte-identical group may be APFS
// clones, where deleting a copy frees nothing, or real duplicates, where it
// frees everything but one — and stat cannot tell which. Folder totals and
// extension totals therefore stay exactly as the filesystem reports them,
// matching du and Finder, and the redundant bytes are carried alongside as a
// separate dimension the UI can subtract to.
func (c *Collector) ApplyDuplicates(result duplicates.Result) {
	c.duplicateBytes = result.DuplicateBytes
	c.duplicateFiles = result.DuplicateFiles

	for i := range c.entries {
		member, ok := result.Membership[c.entries[i].Path]
		if !ok {
			continue
		}
		fingerprint := member.Fingerprint
		copies := member.Copies
		isCopy := result.DuplicatePaths[c.entries[i].Path]

		c.entries[i].DuplicateGroup = &fingerprint
		c.entries[i].DuplicateCopies = &copies
		c.entries[i].IsDuplicateCopy = &isCopy
	}

	// Per-directory attribution is what lets the folder tree and the treemap
	// offer a unique reading; the rollup to ancestors happens in BuildFolderTree.
	for dir, bytes := range result.ByDirectory {
		if accum, ok := c.dirs[dir]; ok {
			accum.DuplicateOwnSize += bytes
		}
	}

	// Store each fingerprint with the candidate that produced it, so the cache
	// can hand it back next time. Without this the duplicate pass would
	// dominate a warm scan — it is the only part that reads file contents.
	for _, delta := range c.freshDeltas {
		for i := range delta.Candidates {
			if value, ok := result.FingerprintOf[delta.Candidates[i].Path]; ok {
				stored := value
				delta.Candidates[i].Fingerprint = &stored
			}
		}
	}
}
