package mirror

import (
	"fmt"
	"sort"
)

func (c Candidate) Score() CandidateScore {
	score := CandidateScore{
		SampleCount: len(c.Heartbeats),
		LastStatus:  -1,
	}

	var pingSum int64
	for idx, heartbeat := range c.Heartbeats {
		if idx == len(c.Heartbeats)-1 {
			score.LastStatus = heartbeat.Status
		}
		if heartbeat.Status == 1 {
			score.SuccessCount++
			pingSum += heartbeat.Ping
		}
	}
	if score.SuccessCount > 0 {
		score.AveragePing = pingSum / int64(score.SuccessCount)
	}
	return score
}

func applyHeartbeats(candidates []Candidate, heartbeatList map[string][]Heartbeat) []Candidate {
	enriched := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.MonitorID > 0 {
			candidate.Heartbeats = heartbeatList[fmt.Sprintf("%d", candidate.MonitorID)]
		}
		enriched = append(enriched, candidate)
	}
	return enriched
}

func rankCandidates(candidates []Candidate) []Candidate {
	filtered := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		score := candidate.Score()
		if score.LastStatus == 1 || score.SampleCount == 0 {
			filtered = append(filtered, candidate)
		}
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		left := filtered[i].Score()
		right := filtered[j].Score()
		if left.SampleCount == 0 || right.SampleCount == 0 {
			if left.SampleCount != right.SampleCount {
				return left.SampleCount > right.SampleCount
			}
			return filtered[i].BaseURL < filtered[j].BaseURL
		}
		leftRate := successRate(left)
		rightRate := successRate(right)
		if leftRate != rightRate {
			return leftRate > rightRate
		}
		if left.AveragePing != right.AveragePing {
			return left.AveragePing < right.AveragePing
		}
		return filtered[i].BaseURL < filtered[j].BaseURL
	})
	return filtered
}

func successRate(score CandidateScore) float64 {
	if score.SampleCount == 0 {
		return 0
	}
	return float64(score.SuccessCount) / float64(score.SampleCount)
}
