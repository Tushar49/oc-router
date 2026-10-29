package config

import "testing"

func TestUsageConfigDefaultsAndValidation(t *testing.T) {
	usage := UsageConfig{}
	if err := usage.Normalize(); err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if usage.RetentionDays != 365 || usage.BatchSize != 100 || usage.FlushIntervalMS != 100 || usage.QueryTimeoutMS != 5000 {
		t.Fatalf("unexpected defaults: %+v", usage)
	}
	invalid := []UsageConfig{
		{RetentionDays: 6},
		{RetentionDays: 3651},
		{BatchSize: 1001},
		{FlushIntervalMS: 5},
		{QueryTimeoutMS: 5},
	}
	for _, candidate := range invalid {
		if err := candidate.Normalize(); err == nil {
			t.Fatalf("expected validation error for %+v", candidate)
		}
	}
}
