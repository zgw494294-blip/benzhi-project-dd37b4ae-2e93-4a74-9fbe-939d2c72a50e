package domain

import (
	"fmt"
	"strings"
)

func validateSourceIdentity(scientificName, location, collector, collectionDate, condition, lot string) error {
	fields := []struct {
		name  string
		value string
	}{
		{name: "学名", value: scientificName},
		{name: "采集地点", value: location},
		{name: "采集人", value: collector},
		{name: "采集日期", value: collectionDate},
		{name: "保藏条件", value: condition},
		{name: "批次编码", value: lot},
	}
	for _, field := range fields {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%w：%s不能为空", ErrInvalid, field.name)
		}
	}
	return nil
}
