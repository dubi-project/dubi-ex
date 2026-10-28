package cron

func collectSpecs(job Job) []string {
	specs := make([]string, 0, 1+len(job.Schedules))
	if job.Schedule != "" {
		specs = append(specs, job.Schedule)
	}
	specs = append(specs, job.Schedules...)
	uniq := make([]string, 0, len(specs))
	seen := map[string]struct{}{}
	for _, spec := range specs {
		if spec == "" {
			continue
		}
		if _, ok := seen[spec]; ok {
			continue
		}
		seen[spec] = struct{}{}
		uniq = append(uniq, spec)
	}
	return uniq
}
