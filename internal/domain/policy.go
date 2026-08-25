package domain

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var lotCodePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{2,63}$`)
var dayStagePattern = regexp.MustCompile(`^(?:第)?([0-9]+)天$`)

const MaximumPlannedObservationUnits = 4096

type ObservationPolicy struct {
	MaximumMoldCount       int
	MinimumTemperature     float64
	MaximumTemperature     float64
	MaximumHumidityPercent float64
	RequireEvidence        bool
}

func DefaultObservationPolicy() ObservationPolicy {
	return ObservationPolicy{MaximumMoldCount: 2, MinimumTemperature: 5, MaximumTemperature: 40, MaximumHumidityPercent: 90, RequireEvidence: true}
}

func ValidateSource(source SeedSource, now time.Time) error {
	if !lotCodePattern.MatchString(source.LotCode) {
		return fmt.Errorf("%w：批次编码只能包含字母、数字、点、下划线和连字符，长度为3至64", ErrInvalid)
	}
	date, err := time.Parse("2006-01-02", source.CollectionDate)
	if err != nil {
		return fmt.Errorf("%w：采集日期格式应为YYYY-MM-DD", ErrInvalid)
	}
	if date.After(now.Add(24 * time.Hour)) {
		return fmt.Errorf("%w：采集日期不能晚于当前日期", ErrInvalid)
	}
	if len([]rune(strings.TrimSpace(source.CollectionLocation))) < 2 {
		return fmt.Errorf("%w：采集地点过短", ErrInvalid)
	}
	if len([]rune(strings.TrimSpace(source.StorageCondition))) < 2 {
		return fmt.Errorf("%w：保藏条件过短", ErrInvalid)
	}
	return nil
}

func NormalizeLotCode(value string) string { return strings.ToUpper(strings.TrimSpace(value)) }

func NormalizeSourceStatus(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "active", "available", "可用":
		return "active", nil
	case "inactive", "disabled", "停用":
		return "inactive", nil
	case "archived", "sealed", "封存":
		return "archived", nil
	default:
		return "", fmt.Errorf("%w：种源状态必须为可用、停用或封存", ErrInvalid)
	}
}

func ValidateSourceUsable(source SeedSource) error {
	switch source.Status {
	case "active":
		return nil
	case "inactive":
		return fmt.Errorf("%w：种源%s当前为停用状态，不能创建新试验", ErrInvalid, source.ID)
	case "archived":
		return fmt.Errorf("%w：种源%s已经封存，不能创建新试验", ErrInvalid, source.ID)
	default:
		return fmt.Errorf("%w：种源%s状态不可用", ErrInvalid, source.ID)
	}
}

func NormalizeObservationStage(value string) (string, int64, error) {
	v := strings.TrimSpace(value)
	if matches := dayStagePattern.FindStringSubmatch(v); matches != nil {
		day, err := strconv.Atoi(matches[1])
		if err != nil || day < 0 || day > 36500 {
			return "", 0, fmt.Errorf("%w：无法识别观测时间点%s", ErrInvalid, value)
		}
		return fmt.Sprintf("第%d天", day), int64(day), nil
	}
	if date, err := time.Parse("2006-01-02", v); err == nil {
		return date.Format("2006-01-02"), date.Unix() / 86400, nil
	}
	return "", 0, fmt.Errorf("%w：无法识别观测时间点%s，应使用“第N天”或YYYY-MM-DD", ErrInvalid, value)
}

func PreflightTrialDesign(replicates int, schedule []string) ([]string, DesignSummary, error) {
	normalized := make([]string, 0, len(schedule))
	seen := map[string]bool{}
	var previous int64
	mode := ""
	for i, value := range schedule {
		stage, order, err := NormalizeObservationStage(value)
		if err != nil {
			return nil, DesignSummary{}, err
		}
		currentMode := "day"
		if strings.Contains(stage, "-") {
			currentMode = "date"
		}
		if mode != "" && mode != currentMode {
			return nil, DesignSummary{}, fmt.Errorf("%w：观测时间表不能混用第几天和日期形式", ErrInvalid)
		}
		mode = currentMode
		if seen[stage] {
			return nil, DesignSummary{}, fmt.Errorf("%w：观测时间点同义重复：%s", ErrInvalid, stage)
		}
		if i > 0 && order < previous {
			return nil, DesignSummary{}, fmt.Errorf("%w：观测时间点顺序倒置：%s位于%s之后", ErrInvalid, stage, normalized[i-1])
		}
		seen[stage], previous = true, order
		normalized = append(normalized, stage)
	}
	total := replicates * len(normalized)
	if total > MaximumPlannedObservationUnits {
		return nil, DesignSummary{}, fmt.Errorf("%w：计划观测单元%d超过安全上限%d（重复组%d，时间点%d）", ErrInvalid, total, MaximumPlannedObservationUnits, replicates, len(normalized))
	}
	summary := DesignSummary{PlannedStages: append([]string(nil), normalized...), ReplicatesPerStage: replicates, EstimatedRecords: total}
	if len(normalized) > 0 {
		summary.NextObservation = normalized[0]
	}
	return normalized, summary, nil
}

func ValidateTrialDesign(trial GerminationTrial) error {
	seen := map[string]bool{}
	for _, stage := range trial.ObservationSchedule {
		stage = strings.TrimSpace(stage)
		if stage == "" {
			return fmt.Errorf("%w：观测时间点不能为空", ErrInvalid)
		}
		if seen[stage] {
			return fmt.Errorf("%w：观测时间点重复：%s", ErrInvalid, stage)
		}
		seen[stage] = true
	}
	if len(trial.ObservationSchedule) > 64 {
		return fmt.Errorf("%w：观测时间点不能超过64个", ErrInvalid)
	}
	return nil
}

func TrialDesignKey(trial GerminationTrial) string {
	return fmt.Sprintf("%s\x00%.6f\x00%s", strings.ToLower(strings.TrimSpace(trial.ProtocolName)), trial.TemperatureCelsius, strings.Join(trial.ObservationSchedule, "\x00"))
}

func EvaluateObservation(policy ObservationPolicy, observation Observation) []Issue {
	now := time.Now().UTC().Format(time.RFC3339)
	issues := make([]Issue, 0, 4)
	add := func(kind, message string) {
		issues = append(issues, Issue{ObservationID: observation.ID, Kind: kind, Message: message, Status: "open", CreatedAt: now})
	}
	if observation.MoldCount > policy.MaximumMoldCount {
		add("mold", "霉变数量超过允许阈值")
	}
	if observation.TemperatureCelsius < policy.MinimumTemperature || observation.TemperatureCelsius > policy.MaximumTemperature {
		add("temperature", "培养温度超过允许范围")
	}
	if observation.HumidityPercent > policy.MaximumHumidityPercent {
		add("humidity", "环境湿度超过允许阈值")
	}
	if policy.RequireEvidence && strings.TrimSpace(observation.EvidenceRef) == "" {
		add("evidence", "观测缺少照片或证据索引")
	}
	return issues
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func ReviewTransitionAllowed(from, to string) bool {
	allowed := map[string]map[string]bool{"draft": {"approved": true, "returned": true}, "returned": {"approved": true, "returned": true}, "approved": {"returned": true, "frozen": true}, "frozen": {}}
	return allowed[from][to]
}
