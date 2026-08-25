package workflow

import (
	"fmt"
	"time"

	"seedvault/internal/domain"
)

func newID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func observationForIssue(trial domain.GerminationTrial, issueID string) string {
	for _, issue := range trial.Issues {
		if issue.ID == issueID {
			return issue.ObservationID
		}
	}
	return ""
}
