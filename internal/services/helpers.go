package services

// ── Type conversion helpers shared across service aggregations ─────────────────

func toInt64(v interface{}) int64 {
	switch val := v.(type) {
	case int32:
		return int64(val)
	case int64:
		return val
	case float64:
		return int64(val)
	}
	return 0
}

func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int32:
		return float64(val)
	case int64:
		return float64(val)
	}
	return 0
}

func roundTwo(f float64) float64 {
	return float64(int(f*100)) / 100
}
