package domain

type Summary struct {
	TotalObservations, TotalGerminated, TotalMold, ReplicatesObserved int
	GerminationRate                                                   float64
}

func (t GerminationTrial) Summary() Summary {
	summary := Summary{}
	replicates := map[int]bool{}
	for _, observation := range t.Observations {
		summary.TotalObservations++
		summary.TotalGerminated += observation.GerminatedCount
		summary.TotalMold += observation.MoldCount
		replicates[observation.ReplicateNo] = true
	}
	summary.ReplicatesObserved = len(replicates)
	if summary.TotalObservations > 0 {
		summary.GerminationRate = float64(summary.TotalGerminated) / float64(summary.TotalObservations)
	}
	return summary
}
