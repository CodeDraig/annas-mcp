package mirror

import "sort"

func rankCandidates(candidates []Candidate) []Candidate {
	ranked := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		switch candidate.Status {
		case StatusUp, StatusDegraded, StatusProtected:
			ranked = append(ranked, candidate)
		}
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		left := ranked[i]
		right := ranked[j]
		if statusRank(left.Status) != statusRank(right.Status) {
			return statusRank(left.Status) < statusRank(right.Status)
		}
		if left.LatencyMillis == 0 || right.LatencyMillis == 0 {
			if left.LatencyMillis != right.LatencyMillis {
				return left.LatencyMillis != 0
			}
			return left.BaseURL < right.BaseURL
		}
		if left.LatencyMillis != right.LatencyMillis {
			return left.LatencyMillis < right.LatencyMillis
		}
		return left.BaseURL < right.BaseURL
	})
	return ranked
}

func statusRank(status CandidateStatus) int {
	switch status {
	case StatusUp:
		return 0
	case StatusDegraded:
		return 1
	case StatusProtected:
		return 2
	default:
		return 3
	}
}
