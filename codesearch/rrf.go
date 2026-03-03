package codesearch

import "sort"

const rrfK = 60 // standard RRF constant

// MergeRRF combines two ranked result lists using Reciprocal Rank Fusion.
// Results appearing in both lists get boosted. Output is sorted by RRF score descending.
func MergeRRF(vectorResults, ftsResults []scoredChunk) []scoredChunk {
	scores := make(map[int64]float64)

	for rank, sc := range vectorResults {
		scores[sc.ChunkID] += 1.0 / float64(rrfK+rank+1)
	}
	for rank, sc := range ftsResults {
		scores[sc.ChunkID] += 1.0 / float64(rrfK+rank+1)
	}

	merged := make([]scoredChunk, 0, len(scores))
	for id, score := range scores {
		merged = append(merged, scoredChunk{ChunkID: id, Score: score})
	}

	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Score > merged[j].Score
	})

	return merged
}
