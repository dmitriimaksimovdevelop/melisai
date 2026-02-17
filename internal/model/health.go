package model

// ComputeHealthScore computes a 0-100 health score.
// 100 = healthy, 0 = critical.
// Based on USE methodology: deduct points for utilization/saturation/errors.
func ComputeHealthScore(resources map[string]USEMetric, anomalies []Anomaly) int {
	score := 100

	// Deduct for USE metrics
	for resource, use := range resources {
		weight := resourceWeight(resource)

		// Utilization deductions
		if use.Utilization >= 95 {
			score -= int(15 * weight)
		} else if use.Utilization >= 85 {
			score -= int(8 * weight)
		} else if use.Utilization >= 70 {
			score -= int(3 * weight)
		}

		// Saturation deductions
		if use.Saturation > 50 {
			score -= int(15 * weight)
		} else if use.Saturation > 10 {
			score -= int(8 * weight)
		} else if use.Saturation > 1 {
			score -= int(3 * weight)
		}

		// Error deductions
		if use.Errors > 1000 {
			score -= int(10 * weight)
		} else if use.Errors > 100 {
			score -= int(5 * weight)
		} else if use.Errors > 0 {
			score -= int(2 * weight)
		}
	}

	// Deduct for anomalies
	for _, a := range anomalies {
		switch a.Severity {
		case "critical":
			score -= 10
		case "warning":
			score -= 5
		}
	}

	// Clamp to [0, 100]
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score
}

// resourceWeight returns the importance weight for a resource.
func resourceWeight(resource string) float64 {
	switch resource {
	case "cpu":
		return 1.5
	case "memory":
		return 1.5
	case "disk":
		return 1.0
	case "network":
		return 1.0
	case "container_cpu", "container_memory":
		return 1.2
	default:
		return 0.5
	}
}
